package pix

import (
	"sort"

	"github.com/bluescreen10/pix/glm"
	"github.com/bluescreen10/pix/gpu"
)

// drawList is a Scene's per-scene GPU draw/cull state: the world-matrix buffer, the
// drawable table, the per-batch indirect args / regions / compacted-visible buffer,
// and the cull/draw root buffers. It is owned by the Scene (so switching scenes
// swaps GPU state), allocated through the Scene's gpu.Backend, and populated by the
// Renderer (which supplies geometry descriptors). No bind groups — every buffer is a
// BDA carried in the compute/graphics root structs.
type drawList struct {
	backend gpu.Backend

	batches          []batch
	runs             []pipelineRun // contiguous same-pipeline batch spans (one MDI call each)
	regions          []uint32      // regionBase per batch (GPU mirror)
	template         []indirectCmd // per-batch indirect args (reset each frame)
	pipeBuf          []uint32      // scratch: per-mesh pipeline ids resolved each frame
	batchedPipelines []uint32      // the pipeline assignment the current batches were built from
	visCap           uint32
	numInst          uint32
	worldCap         uint32

	worldBuf        gpu.Buffer // per-node world matrices (models), indexed by node slot
	drawableBuf     gpu.Buffer
	indirectBuf     gpu.Buffer
	regionBuf       gpu.Buffer
	visibleBuf      gpu.Buffer
	cullRootBuf     gpu.Buffer
	drawRootBuf     gpu.Buffer // one drawRoot per pipeline run
	lightingRootBuf gpu.Buffer // one lightingRoot per frame (the deferred lighting pass)
	skinRootBuf     gpu.Buffer // one skinRoot per skinned mesh per frame

	// Shadow views: one extra cull + depth pass per shadow-casting light. They share
	// the drawable/world/region tables (culled from the same drawables) but each owns
	// its indirect args, compacted-visible buffer and root buffers. Pooled and grown
	// on demand; each view's buffers grow to fit the main batch/visible layout.
	shadowViews []drawView
}

// drawView is one shadow view's private GPU state: its own indirect args and
// compacted-visible buffer (culled independently from the light's frustum) plus the
// cull/depth-pass root buffers. The drawable/world/region tables it reads are the
// drawList's shared ones.
type drawView struct {
	indirectBuf gpu.Buffer
	visibleBuf  gpu.Buffer
	cullRootBuf gpu.Buffer
	drawRootBuf gpu.Buffer // single shadowRoot for the depth pass
}

func newDrawList(b gpu.Backend) *drawList {
	return &drawList{backend: b}
}

// sync uploads the scene's world matrices into the models buffer (growing it as the
// node count grows). transformID in drawables indexes this array. The buffer is
// host-visible per-frame streaming, so this is a direct write (no staging uploader).
func (d *drawList) sync(world []glm.Mat4f) {
	n := uint32(len(world))
	if n > d.worldCap {
		if d.worldBuf.Valid() {
			d.backend.Free(d.worldBuf)
		}
		d.worldCap = max(n*2, 1)
		d.worldBuf = d.backend.Alloc(uint64(d.worldCap)*64, gpu.MemoryHost, "world")
	}
	if n > 0 {
		writeAt(d.worldBuf, 0, toBytes(world))
	}
}

// rebuild groups drawables into (pipeline, geometry) batches (one indirect command
// each), orders them so same-pipeline batches are contiguous (one multi-draw-indirect
// call per pipeline), lays out their visible-buffer regions, fills the indirect
// template (indexCount/firstIndex from the geometry, firstInstance = the region base),
// resizes buffers, and uploads the drawable + region tables. Called on structural
// change. pipelines[i] is drawables[i]'s draw pipeline.
func (d *drawList) rebuild(drawables []gpuDrawable, pipelines []uint32, materials []Material, geo *geometrySystem) {
	type key struct{ pipeline, geo uint32 }

	// First pass: unique (pipeline, geometry) batches + their instance counts, and a
	// representative material per pipeline (any drawable using that pipeline).
	index := map[key]uint32{}
	rep := map[uint32]Material{}
	var raw []batch
	var counts []uint32
	for i := range drawables {
		k := key{pipelines[i], drawables[i].geometryID}
		if _, ok := rep[k.pipeline]; !ok {
			rep[k.pipeline] = materials[i]
		}
		bid, ok := index[k]
		if !ok {
			bid = uint32(len(raw))
			index[k] = bid
			raw = append(raw, batch{pipeline: k.pipeline, geometryID: k.geo})
			counts = append(counts, 0)
		}
		counts[bid]++
	}

	// Remember the pipeline assignment these batches were built from (change detection).
	d.batchedPipelines = append(d.batchedPipelines[:0], pipelines...)

	// Order batches: opaque pipelines before transparent (blended) ones so blending
	// composites over the opaque scene; within each group, by pipeline (so a pipeline's
	// commands stay contiguous for one MDI call).
	transparent := func(pid uint32) bool { return rep[pid].Blend() != BlendOpaque }
	order := make([]uint32, len(raw))
	for i := range order {
		order[i] = uint32(i)
	}
	sort.SliceStable(order, func(a, b int) bool {
		pa, pb := raw[order[a]].pipeline, raw[order[b]].pipeline
		if ta, tb := transparent(pa), transparent(pb); ta != tb {
			return !ta // opaque first
		}
		return pa < pb
	})
	remap := make([]uint32, len(raw)) // old batch id -> new (sorted) id
	for newID, oldID := range order {
		remap[oldID] = uint32(newID)
	}

	d.batches = d.batches[:0]
	d.regions = d.regions[:0]
	d.template = d.template[:0]
	d.runs = d.runs[:0]
	var base uint32
	for _, oldID := range order {
		b := raw[oldID]
		padded := (counts[oldID] + regionAlign - 1) &^ (regionAlign - 1)
		b.regionBase = base
		b.regionCap = padded
		d.batches = append(d.batches, b)
		d.regions = append(d.regions, base)
		desc := geo.Desc(b.geometryID)
		d.template = append(d.template, indirectCmd{
			indexCount: desc.IndexCount, firstIndex: desc.IndexBase, firstInstance: base,
		})
		// Extend or start a pipeline run.
		if n := len(d.runs); n > 0 && d.runs[n-1].pipeline == b.pipeline {
			d.runs[n-1].count++
		} else {
			d.runs = append(d.runs, pipelineRun{pipeline: b.pipeline, firstBatch: uint32(len(d.batches) - 1), count: 1, mat: rep[b.pipeline]})
		}
		base += padded
	}

	// Second pass: set each drawable's batchID to its (remapped) batch.
	for i := range drawables {
		drawables[i].batchID = remap[index[key{pipelines[i], drawables[i].geometryID}]]
	}

	d.visCap = base
	if d.visCap == 0 {
		d.visCap = regionAlign
	}
	d.numInst = uint32(len(drawables))

	d.ensureBuffers()
	if len(drawables) > 0 {
		writeAt(d.drawableBuf, 0, toBytes(drawables))
	}
	if len(d.regions) > 0 {
		writeAt(d.regionBuf, 0, toBytes(d.regions))
	}
}

// ensureBuffers owns every buffer the draw list allocates. The count-dependent ones
// (instances, batches, pipeline runs) only grow: a buffer that is already big enough
// is kept, so a steady-state scene reallocates nothing even though rebuild runs
// whenever the mesh set or a pipeline assignment changes. Every writer bounds itself
// by the current count rather than the buffer's capacity, so the slack is harmless.
// The two root buffers are single fixed-size structs — allocated once, never resized.
func (d *drawList) ensureBuffers() {
	nb := max(uint32(len(d.batches)), 1)
	nr := max(uint32(len(d.runs)), 1)
	ni := max(d.numInst, 1)

	if size := uint64(ni) * uint64(drawableSize); !d.drawableBuf.Valid() || d.drawableBuf.Size < size {
		if d.drawableBuf.Valid() {
			d.backend.Free(d.drawableBuf)
		}
		d.drawableBuf = d.backend.Alloc(size, gpu.MemoryHost, "drawables")
	}
	if size := uint64(nb) * uint64(indirectSize); !d.indirectBuf.Valid() || d.indirectBuf.Size < size {
		if d.indirectBuf.Valid() {
			d.backend.Free(d.indirectBuf)
		}
		d.indirectBuf = d.backend.Alloc(size, gpu.MemoryHost, "indirect")
	}
	if size := uint64(nb) * 4; !d.regionBuf.Valid() || d.regionBuf.Size < size {
		if d.regionBuf.Valid() {
			d.backend.Free(d.regionBuf)
		}
		d.regionBuf = d.backend.Alloc(size, gpu.MemoryHost, "regions")
	}
	if size := uint64(d.visCap) * 4; !d.visibleBuf.Valid() || d.visibleBuf.Size < size {
		if d.visibleBuf.Valid() {
			d.backend.Free(d.visibleBuf)
		}
		d.visibleBuf = d.backend.Alloc(size, gpu.MemoryHost, "visible")
	}
	if size := uint64(nr) * drawRootSize; !d.drawRootBuf.Valid() || d.drawRootBuf.Size < size {
		if d.drawRootBuf.Valid() {
			d.backend.Free(d.drawRootBuf)
		}
		d.drawRootBuf = d.backend.Alloc(size, gpu.MemoryHost, "draw-roots")
	}
	// One cullRoot for the main view; one lightingRoot per frame. Fixed size, so
	// validity alone decides — there is no size that could grow.
	if !d.cullRootBuf.Valid() {
		d.cullRootBuf = d.backend.Alloc(cullRootSize, gpu.MemoryHost, "cull-root")
	}
	if !d.lightingRootBuf.Valid() {
		d.lightingRootBuf = d.backend.Alloc(lightingRootSize, gpu.MemoryHost, "lighting-root")
	}
}

// ensureShadowViews grows the shadow-view pool to at least n views and makes sure each
// view's indirect + visible buffers are big enough for the current batch/visible
// layout. Same grow-only rule as ensureBuffers: a view already large enough is left
// alone, so this is free once the pool has settled. Root buffers are fixed size and
// allocated with the view.
func (d *drawList) ensureShadowViews(n int) {
	for len(d.shadowViews) < n {
		d.shadowViews = append(d.shadowViews, drawView{
			cullRootBuf: d.backend.Alloc(cullRootSize, gpu.MemoryHost, "shadow-cull-root"),
			drawRootBuf: d.backend.Alloc(shadowRootSize, gpu.MemoryHost, "shadow-draw-root"),
		})
	}
	indirect := uint64(max(uint32(len(d.batches)), 1)) * uint64(indirectSize)
	visible := uint64(d.visCap) * 4
	for i := range d.shadowViews {
		v := &d.shadowViews[i]
		if !v.indirectBuf.Valid() || v.indirectBuf.Size < indirect {
			if v.indirectBuf.Valid() {
				d.backend.Free(v.indirectBuf)
			}
			v.indirectBuf = d.backend.Alloc(indirect, gpu.MemoryHost, "shadow-indirect")
		}
		if !v.visibleBuf.Valid() || v.visibleBuf.Size < visible {
			if v.visibleBuf.Valid() {
				d.backend.Free(v.visibleBuf)
			}
			v.visibleBuf = d.backend.Alloc(visible, gpu.MemoryHost, "shadow-visible")
		}
	}
}

func (d *drawList) batchCount() int { return len(d.batches) }

// ensureSkinRoots makes sure the per-frame skin-root buffer is big enough for n
// skinned-mesh dispatches (grow-only, same rule as ensureBuffers).
func (d *drawList) ensureSkinRoots(n int) {
	size := uint64(n) * skinRootSize
	if d.skinRootBuf.Valid() && d.skinRootBuf.Size >= size {
		return
	}
	if d.skinRootBuf.Valid() {
		d.backend.Free(d.skinRootBuf)
	}
	d.skinRootBuf = d.backend.Alloc(size, gpu.MemoryHost, "skin-roots")
}

func (d *drawList) destroy() {
	bufs := []gpu.Buffer{d.worldBuf, d.drawableBuf, d.indirectBuf, d.regionBuf, d.visibleBuf, d.cullRootBuf, d.drawRootBuf, d.lightingRootBuf, d.skinRootBuf}
	for _, v := range d.shadowViews {
		bufs = append(bufs, v.indirectBuf, v.visibleBuf, v.cullRootBuf, v.drawRootBuf)
	}
	for _, b := range bufs {
		if b.Valid() {
			d.backend.Free(b)
		}
	}
	*d = drawList{}
}
