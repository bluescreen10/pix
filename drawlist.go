package pix

import (
	"sort"

	"github.com/bluescreen10/gamekit/gpu"
	"github.com/bluescreen10/gamekit/utils"
	"github.com/bluescreen10/pix/geometries"
	"github.com/bluescreen10/pix/glm"
	"github.com/bluescreen10/pix/materials"
	"github.com/bluescreen10/pix/scenes"
)

// drawList is one packet source's GPU draw/cull state: the world-matrix buffer, the
// drawable table, the per-batch indirect args / regions / compacted-visible buffer,
// and the cull/draw root buffers. The renderer owns it, keyed by SourceID (see
// renderState), and builds it from the packets that source publishes — a producer
// never sees any of it. No bind groups: every buffer is a BDA carried in the
// compute/graphics root structs.
type drawList struct {
	backend gpu.Backend

	batches        []batch
	runs           []pipelineRun // contiguous same-pipeline batch spans (one MDI call each)
	regions        []uint32      // regionBase per batch (GPU mirror)
	template       []indirectCmd // per-batch indirect args (reset each frame)
	roots          []drawRoot    // scratch: one drawRoot per pipeline run, pushed inline
	batchedMatPipe []uint32      // the per-material pipeline assignment the current batches were built from

	// Per DISTINCT material of the current packet, refreshed every frame: its pipeline,
	// the pool holding its record, and its blend mode. Indexed by MeshPacket.Material.
	matPipe  []uint32
	matPool  []*materials.Pool
	matBlend []materials.BlendMode

	// The packet's mesh table expanded into flat draw records — one per instance per
	// LOD level — plus, parallel to it, each record's pipeline and the material slot it
	// came from. Rebuilt only when the batch layout is.
	drawables   []gpuDrawable
	pipeBuf     []uint32
	drawMatSlot []uint32
	// lodEntries is the renderer's own GPU LOD-config table, derived from the packet
	// (index 0 reserved, so a drawable's lodID of 0 means "not LOD-tagged"). The scene
	// no longer supplies it: hysteresis and level boundaries reach here as mesh
	// description, and what the cull shader reads is this side's business.
	lodEntries []gpuLODEntry
	// builtRevision is the mesh-table revision the current batch layout was built from.
	builtRevision uint64
	visCap        uint32
	numInst       uint32
	worldCap      uint32
	lodCount      uint32 // len(scenes.Scene.lodEntries) as of the last rebuild
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
// (see FramePacket.InstanceTransforms and drawList.expand). The buffer is
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
		d.worldBuf.Write(utils.ToBytesSlice(world), 0)
	}
	if len(instances) > 0 {
		d.worldBuf.Write(utils.ToBytesSlice(instances), uint64(len(world))*64)
	}
}

// expand turns the packet's object-shaped mesh table into the flat draw records the
// batcher and the GPU cull pass work on: one per instance per LOD level, each tagged
// with its pipeline and the material slot it came from. It also derives the GPU LOD
// config table, because level boundaries reach the renderer as mesh description and
// what the cull shader reads is the renderer's own business.
//
// The iteration order is a contract, not an implementation detail: instances outer,
// levels inner, objects in table order. Anything reproducing this expansion — a test,
// a second consumer — has to agree, or the records line up with the wrong materials.
func (d *drawList) expand(p *scenes.FramePacket) {
	d.drawables = d.drawables[:0]
	d.pipeBuf = d.pipeBuf[:0]
	d.drawMatSlot = d.drawMatSlot[:0]
	// Index 0 is reserved and never read: a drawable's lodID of 0 means "not LOD-tagged".
	d.lodEntries = append(d.lodEntries[:0], gpuLODEntry{})

	for _, m := range p.Meshes.Data {
		lodID := uint32(0)
		if m.LODRange.Count > 0 {
			e := gpuLODEntry{hysteresis: m.LODHysteresis, levelCount: m.LODRange.Count + 1}
			// boundaries[i] is where level i ends and level i+1 begins — which is
			// level i+1's own MinDistance. The last level has no upper bound.
			for i := uint32(0); i < m.LODRange.Count && int(i) < len(e.boundaries); i++ {
				e.boundaries[i] = p.LODs.Data[m.LODRange.First+i].MinDistance
			}
			lodID = uint32(len(d.lodEntries))
			d.lodEntries = append(d.lodEntries, e)
		}

		bounds := [4]float32{m.Bounds.Center[0], m.Bounds.Center[1], m.Bounds.Center[2], m.Bounds.Radius}
		var flags uint32
		if m.Flags&scenes.RenderCastsShadow != 0 {
			flags |= DrawableCastsShadow
		}
		if m.Flags&scenes.RenderReceivesShadow != 0 {
			flags |= DrawableReceivesShadow
		}

		for j := uint32(0); j < m.Transforms.Count; j++ {
			for lvl := uint32(0); lvl <= m.LODRange.Count; lvl++ {
				geo, mat := m.Geometry, m.Material
				if lvl > 0 {
					l := p.LODs.Data[m.LODRange.First+lvl-1]
					geo, mat = l.Geometry, l.Material
				}
				d.drawables = append(d.drawables, gpuDrawable{
					bounds:      bounds,
					transformID: m.Transforms.First + j,
					geometryID:  geo.Slot,
					materialID:  p.Materials.Data[mat].Slot,
					flags:       flags,
					lodID:       lodID,
					lodLevel:    lvl,
				})
				d.pipeBuf = append(d.pipeBuf, d.matPipe[mat])
				d.drawMatSlot = append(d.drawMatSlot, mat)
			}
		}
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
func (d *drawList) rebuild(geometryStore *geometries.Store) {
	type key struct{ pipeline, geo uint32 }
	d.rebuilds++
	drawables, pipelines, lodEntries := d.drawables, d.pipeBuf, d.lodEntries

	// First pass: unique (pipeline, geometry) batches + their instance counts, and a
	// representative material per pipeline (any drawable using it), kept as a slot in
	// the packet's distinct material set rather than as a handle.
	index := map[key]uint32{}
	rep := map[uint32]uint32{}
	var raw []batch
	var counts []uint32
	for i := range drawables {
		k := key{pipelines[i], drawables[i].geometryID}
		if _, ok := rep[k.pipeline]; !ok {
			rep[k.pipeline] = d.drawMatSlot[i]
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
		return d.matBlend[rep[pid]] != materials.BlendOpaque
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
			d.runs = append(d.runs, pipelineRun{pipeline: b.pipeline, firstBatch: uint32(len(d.batches) - 1), count: 1, pool: d.matPool[rep[b.pipeline]]})
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
		d.drawableBuf.Write(utils.ToBytesSlice(drawables), 0)
	}
	if len(d.regions) > 0 {
		d.regionBuf.Write(utils.ToBytesSlice(d.regions), 0)
	}
	if len(lodEntries) > 0 {
		d.lodTableBuf.Write(utils.ToBytesSlice(lodEntries), 0)
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
	d.prevLevelBuf.Write(utils.ToBytesSlice(d.lodScratch), 0)
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

func (d *drawList) batchCount() int {
	return len(d.batches)
}

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
