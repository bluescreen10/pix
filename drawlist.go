package pix

import (
	"sort"

	"github.com/bluescreen10/gamekit/gpu"
	"github.com/bluescreen10/pix/geometries"
	"github.com/bluescreen10/pix/glm"
	"github.com/bluescreen10/pix/materials"
)

// drawList is a Scene's per-scene GPU draw/cull state: the world-matrix buffer, the
// drawable table, the per-batch indirect args / regions / compacted-visible buffer,
// and the cull/draw root buffers. It is owned by the Scene (so switching scenes
// swaps GPU state), allocated through the Scene's gpu.Backend, and populated by the
// Renderer (which supplies geometry descriptors). No bind groups — every buffer is a
// BDA carried in the compute/graphics root structs.
type drawList struct {
	backend gpu.Backend

	batches        []batch
	runs           []pipelineRun // contiguous same-pipeline batch spans (one MDI call each)
	regions        []uint32      // regionBase per batch (GPU mirror)
	template       []indirectCmd // per-batch indirect args (reset each frame)
	pipeBuf        []uint32      // scratch: per-drawable pipeline ids, expanded from matPipe
	roots          []drawRoot    // scratch: one drawRoot per pipeline run, pushed inline
	matPipe        []uint32      // scratch: one pipeline id per DISTINCT material, resolved each frame
	batchedMatPipe []uint32      // the per-material pipeline assignment the current batches were built from
	visCap         uint32
	numInst        uint32
	worldCap       uint32
	lodCount       uint32 // len(Scene.lodEntries) as of the last rebuild
	// rebuilds counts how many times the batch layout has been rebuilt. A diagnostic:
	// a steady-state frame must not advance it, and neither must an edit that changes
	// only a material's record (a tint), as opposed to its pipeline.
	rebuilds uint64

	worldBuf    gpu.Buffer // per-node world matrices (models), indexed by node slot
	drawableBuf gpu.Buffer
	indirectBuf gpu.Buffer
	regionBuf   gpu.Buffer
	visibleBuf  gpu.Buffer

	// LOD state — see the LOD spec (project memory). lodTableBuf is a straight,
	// per-rebuild upload of the scene's lodEntries (indexed by gpuDrawable.lodID).
	// prevLevelBuf is one uint32 per worldBuf slot (nodes, then instance transforms —
	// the same transformID address space), holding which level was selected there
	// last frame; scene_cull.comp both reads and writes it, so unlike every other
	// buffer here it persists ACROSS ordinary frames and is only reset to the
	// lodNoneSentinel on a structural rebuild (see rebuild's tail).
	lodTableBuf  gpu.Buffer
	prevLevelBuf gpu.Buffer
	lodScratch   []uint32 // reused sentinel-fill scratch for prevLevelBuf resets

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
}

func newDrawList(b gpu.Backend) *drawList {
	return &drawList{backend: b}
}

// sync uploads the scene's world matrices, followed immediately by every
// InstancedMesh's per-instance transforms, into one contiguous models buffer
// (growing it as needed). transformID in drawables indexes this combined array —
// an InstancedMesh's drawables address their slice as if it sits right after world
// (see Scene.collectDrawables and Scene.instanceTransforms). The buffer is
// host-visible per-frame streaming, so this is a direct write (no staging uploader).
func (d *drawList) sync(world, instances []glm.Mat4f) {
	n := uint32(len(world) + len(instances))
	if n > d.worldCap {
		if d.worldBuf.IsValid() {
			d.backend.Free(d.worldBuf)
		}
		d.worldCap = max(n*2, 1)
		d.worldBuf = d.backend.Alloc(uint64(d.worldCap)*64, gpu.MemoryHost, "world")
	}
	if len(world) > 0 {
		writeAt(d.worldBuf, 0, toBytes(world))
	}
	if len(instances) > 0 {
		writeAt(d.worldBuf, uint32(len(world))*64, toBytes(instances))
	}
}

// rebuild groups drawables into (pipeline, geometry) batches (one indirect command
// each), orders them so same-pipeline batches are contiguous (one multi-draw-indirect
// call per pipeline), lays out their visible-buffer regions, fills the indirect
// template (indexCount/firstIndex from the geometry, firstInstance = the region base),
// resizes buffers, and uploads the drawable + region + LOD tables (and resets
// prevLevelBuf's hysteresis state — see the field doc). Called on structural change.
// pipelines[i] is drawables[i]'s draw pipeline; lodEntries is the scene's lodEntries,
// uploaded verbatim (indexed directly by gpuDrawable.lodID, no reordering needed).
//
// Which pipeline assignment the batches were built from is remembered by syncDrawList,
// per distinct material rather than per drawable — see batchedMatPipe.
func (d *drawList) rebuild(drawables []gpuDrawable, pipelines []uint32, mats []materials.Material, matIndex []uint32, geometryStore *geometries.Store, lodEntries []gpuLODEntry) {
	type key struct{ pipeline, geo uint32 }
	d.rebuilds++

	// First pass: unique (pipeline, geometry) batches + their instance counts, and a
	// representative material per pipeline (any drawable using that pipeline). mats is
	// the distinct set, so matIndex[i] is where drawables[i]'s material sits in it.
	index := map[key]uint32{}
	rep := map[uint32]materials.Material{}
	var raw []batch
	var counts []uint32
	for i := range drawables {
		k := key{pipelines[i], drawables[i].geometryID}
		if _, ok := rep[k.pipeline]; !ok {
			rep[k.pipeline] = mats[matIndex[i]]
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

	// Order batches: opaque pipelines before transparent (blended) ones so blending
	// composites over the opaque scene; within each group, by pipeline (so a pipeline's
	// commands stay contiguous for one MDI call).
	transparent := func(pid uint32) bool {
		m := rep[pid]
		return m.Pool().Blend(m.ID().Slot) != materials.BlendOpaque
	}
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
		d.template = append(d.template, indirectCmd{
			indexCount: geometryStore.IndexCount(b.geometryID), firstIndex: geometryStore.IndexBase(b.geometryID), firstInstance: base,
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

	d.lodCount = uint32(len(lodEntries))
	d.ensureBuffers()
	if len(drawables) > 0 {
		writeAt(d.drawableBuf, 0, toBytes(drawables))
	}
	if len(d.regions) > 0 {
		writeAt(d.regionBuf, 0, toBytes(d.regions))
	}
	if len(lodEntries) > 0 {
		writeAt(d.lodTableBuf, 0, toBytes(lodEntries))
	}
	// Reset every slot's hysteresis state: a structural change may have added,
	// removed, or reassigned lodID/lodLevel, so stale "level shown last frame" state
	// could otherwise pick a level that no longer matches this drawable set. Losing
	// hysteresis's stickiness for one frame right after a rebuild is an accepted
	// tradeoff (see the LOD spec) — rebuilds are already disruptive.
	n := max(d.worldCap, 1)
	if uint32(cap(d.lodScratch)) < n {
		d.lodScratch = make([]uint32, n)
	}
	d.lodScratch = d.lodScratch[:n]
	for i := range d.lodScratch {
		d.lodScratch[i] = lodNoneSentinel
	}
	writeAt(d.prevLevelBuf, 0, toBytes(d.lodScratch))
}

// ensureBuffers owns every buffer the draw list allocates. The count-dependent ones
// (instances, batches, pipeline runs) only grow: a buffer that is already big enough
// is kept, so a steady-state scene reallocates nothing even though rebuild runs
// whenever the mesh set or a pipeline assignment changes. Every writer bounds itself
// by the current count rather than the buffer's capacity, so the slack is harmless.
func (d *drawList) ensureBuffers() {
	nb := max(uint32(len(d.batches)), 1)
	ni := max(d.numInst, 1)

	if size := uint64(ni) * uint64(drawableSize); !d.drawableBuf.IsValid() || d.drawableBuf.Size < size {
		if d.drawableBuf.IsValid() {
			d.backend.Free(d.drawableBuf)
		}
		d.drawableBuf = d.backend.Alloc(size, gpu.MemoryHost, "drawables")
	}
	if size := uint64(nb) * uint64(indirectSize); !d.indirectBuf.IsValid() || d.indirectBuf.Size < size {
		if d.indirectBuf.IsValid() {
			d.backend.Free(d.indirectBuf)
		}
		d.indirectBuf = d.backend.Alloc(size, gpu.MemoryHost, "indirect")
	}
	if size := uint64(nb) * 4; !d.regionBuf.IsValid() || d.regionBuf.Size < size {
		if d.regionBuf.IsValid() {
			d.backend.Free(d.regionBuf)
		}
		d.regionBuf = d.backend.Alloc(size, gpu.MemoryHost, "regions")
	}
	if size := uint64(d.visCap) * 4; !d.visibleBuf.IsValid() || d.visibleBuf.Size < size {
		if d.visibleBuf.IsValid() {
			d.backend.Free(d.visibleBuf)
		}
		d.visibleBuf = d.backend.Alloc(size, gpu.MemoryHost, "visible")
	}
	nl := max(d.lodCount, 1)
	if size := uint64(nl) * uint64(lodEntrySize); !d.lodTableBuf.IsValid() || d.lodTableBuf.Size < size {
		if d.lodTableBuf.IsValid() {
			d.backend.Free(d.lodTableBuf)
		}
		d.lodTableBuf = d.backend.Alloc(size, gpu.MemoryHost, "lod-table")
	}
	// prevLevelBuf's whole content is overwritten (see rebuild's sentinel-fill) every
	// time this is called, so — unlike every other buffer here — reallocating it on
	// grow rather than preserving old contents is fine: rebuild always follows up with
	// a full rewrite regardless.
	nw := max(d.worldCap, 1)
	if size := uint64(nw) * 4; !d.prevLevelBuf.IsValid() || d.prevLevelBuf.Size < size {
		if d.prevLevelBuf.IsValid() {
			d.backend.Free(d.prevLevelBuf)
		}
		d.prevLevelBuf = d.backend.Alloc(size, gpu.MemoryHost, "lod-prev-level")
	}
}

// ensureShadowViews grows the shadow-view pool to at least n views and makes sure each
// view's indirect + visible buffers are big enough for the current batch/visible
// layout. Same grow-only rule as ensureBuffers: a view already large enough is left
// alone, so this is free once the pool has settled.
func (d *drawList) ensureShadowViews(n int) {
	for len(d.shadowViews) < n {
		d.shadowViews = append(d.shadowViews, drawView{})
	}
	indirect := uint64(max(uint32(len(d.batches)), 1)) * uint64(indirectSize)
	visible := uint64(d.visCap) * 4
	for i := range d.shadowViews {
		v := &d.shadowViews[i]
		if !v.indirectBuf.IsValid() || v.indirectBuf.Size < indirect {
			if v.indirectBuf.IsValid() {
				d.backend.Free(v.indirectBuf)
			}
			v.indirectBuf = d.backend.Alloc(indirect, gpu.MemoryHost, "shadow-indirect")
		}
		if !v.visibleBuf.IsValid() || v.visibleBuf.Size < visible {
			if v.visibleBuf.IsValid() {
				d.backend.Free(v.visibleBuf)
			}
			v.visibleBuf = d.backend.Alloc(visible, gpu.MemoryHost, "shadow-visible")
		}
	}
}

func (d *drawList) batchCount() int { return len(d.batches) }

func (d *drawList) destroy() {
	bufs := []gpu.Buffer{d.worldBuf, d.drawableBuf, d.indirectBuf, d.regionBuf, d.visibleBuf, d.lodTableBuf, d.prevLevelBuf}
	for _, v := range d.shadowViews {
		bufs = append(bufs, v.indirectBuf, v.visibleBuf)
	}
	for _, b := range bufs {
		if b.IsValid() {
			d.backend.Free(b)
		}
	}
	*d = drawList{}
}
