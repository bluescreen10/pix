package pix

import (
	"sort"
	"unsafe"

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

	worldBuf    gpu.Buffer // per-node world matrices (models), indexed by node slot
	drawableBuf gpu.Buffer
	indirectBuf gpu.Buffer
	regionBuf   gpu.Buffer
	visibleBuf  gpu.Buffer
	cullRootBuf gpu.Buffer
	drawRootBuf gpu.Buffer // one drawRoot per pipeline run
}

func newDrawList(b gpu.Backend) *drawList {
	return &drawList{
		backend:     b,
		cullRootBuf: b.Alloc(uint64(unsafe.Sizeof(cullRoot{})), gpu.MemoryHost, "cull-root"),
	}
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

func (d *drawList) ensureBuffers() {
	nb := max(uint32(len(d.batches)), 1)
	nr := max(uint32(len(d.runs)), 1)
	ni := max(d.numInst, 1)
	realloc := func(old gpu.Buffer, size uint64, label string) gpu.Buffer {
		if old.Valid() {
			d.backend.Free(old)
		}
		return d.backend.Alloc(size, gpu.MemoryHost, label)
	}
	d.drawableBuf = realloc(d.drawableBuf, uint64(ni)*uint64(drawableSize), "drawables")
	d.indirectBuf = realloc(d.indirectBuf, uint64(nb)*uint64(indirectSize), "indirect")
	d.regionBuf = realloc(d.regionBuf, uint64(nb)*4, "regions")
	d.visibleBuf = realloc(d.visibleBuf, uint64(d.visCap)*4, "visible")
	d.drawRootBuf = realloc(d.drawRootBuf, uint64(nr)*drawRootSize, "draw-roots")
}

func (d *drawList) batchCount() int { return len(d.batches) }

func (d *drawList) destroy() {
	for _, b := range []gpu.Buffer{d.worldBuf, d.drawableBuf, d.indirectBuf, d.regionBuf, d.visibleBuf, d.cullRootBuf, d.drawRootBuf} {
		if b.Valid() {
			d.backend.Free(b)
		}
	}
	*d = drawList{}
}
