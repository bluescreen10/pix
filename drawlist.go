package pix

import (
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

	batches  []batch
	regions  []uint32      // regionBase per batch (GPU mirror)
	template []indirectCmd // per-batch indirect args (reset each frame)
	visCap   uint32
	numInst  uint32
	worldCap uint32

	worldBuf    gpu.Buffer // per-node world matrices (models), indexed by node slot
	drawableBuf gpu.Buffer
	indirectBuf gpu.Buffer
	regionBuf   gpu.Buffer
	visibleBuf  gpu.Buffer
	cullRootBuf gpu.Buffer
	drawRootBuf gpu.Buffer // one drawRoot per batch
}

func newDrawList(b gpu.Backend) *drawList {
	return &drawList{
		backend:     b,
		cullRootBuf: b.Alloc(uint64(unsafe.Sizeof(cullRoot{})), gpu.MemoryHost, "cull-root"),
	}
}

// uploadWorld uploads the scene's world matrices into the models buffer (growing it
// as the node count grows). transformID in drawables indexes this array.
func (d *drawList) uploadWorld(world []glm.Mat4f) {
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

// rebuild groups drawables into (geometry, material) batches, lays out their
// visible-buffer regions, fills the indirect template from geo descriptors, resizes
// the GPU buffers, and uploads the drawable + region tables. Called by the Renderer
// on a structural change (mesh add/remove). Each drawable's batchID is assigned here.
func (d *drawList) rebuild(drawables []gpuDrawable, geo *geometrySystem) {
	type key struct{ geo, mat uint32 }
	index := map[key]uint32{}
	d.batches = d.batches[:0]
	counts := []uint32{}
	for i := range drawables {
		dw := &drawables[i]
		k := key{dw.geometryID, dw.materialID}
		bid, ok := index[k]
		if !ok {
			bid = uint32(len(d.batches))
			index[k] = bid
			d.batches = append(d.batches, batch{geometryID: dw.geometryID, materialID: dw.materialID})
			counts = append(counts, 0)
		}
		counts[bid]++
		dw.batchID = bid
	}

	d.regions = d.regions[:0]
	d.template = d.template[:0]
	var base uint32
	for bid := range d.batches {
		b := &d.batches[bid]
		padded := (counts[bid] + regionAlign - 1) &^ (regionAlign - 1)
		b.regionBase = base
		b.regionCap = padded
		d.regions = append(d.regions, base)
		desc := geo.Desc(b.geometryID)
		d.template = append(d.template, indirectCmd{indexCount: desc.IndexCount, firstIndex: desc.IndexBase})
		base += padded
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
	d.drawRootBuf = realloc(d.drawRootBuf, uint64(nb)*drawRootSize, "draw-roots")
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
