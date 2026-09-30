package pix

import (
	"iter"
	"slices"
	"sort"

	"github.com/bluescreen10/pix/geometries"
	"github.com/bluescreen10/pix/materials"
	"github.com/bluescreen10/pix/scenes"
)

// drawLayout is what to draw and in what order, derived from one packet's mesh table.
//
// Plain CPU-side data. The buffers it is uploaded into belong to the renderState that
// holds it, and it names no pipelines: a batch records the raster state its materials
// share, and the renderer turns that into a pipeline when it draws.
type drawLayout struct {
	// drawables is one record per instance per LOD level. batches groups them by raster
	// state and geometry, one indirect command each, ordered so that batches sharing
	// raster state are adjacent — which is what lets each such span draw as one
	// multi-draw-indirect call (see rasterSpans).
	drawables []gpuDrawable
	batches   []batch

	// template is the GPU's view of the batch order: each batch's indirect arguments,
	// copied fresh into every view's indirect buffer before its cull. firstInstance is
	// also the base of the batch's region in the visible buffer — the cull writes a
	// batch's surviving instances there, and the draw reads them from there.
	template []indirectCmd

	// lods is the renderer's own GPU LOD-config table, indexed by gpuDrawable.lodID.
	// Index 0 is reserved, so a lodID of 0 means "not LOD-tagged".
	lods []gpuLOD

	// visibleCapacity is how many entries a view's visible buffer needs: every batch's
	// region, each rounded up to regionAlign.
	visibleCapacity uint32

	// meshRevision is the packet mesh-table revision this layout was built from.
	meshRevision uint64
}

// batch is one indirect command: the drawables sharing raster state and a geometry.
//
// Raster state is a material pool plus a cull and a blend mode — exactly what selects a
// pipeline — so every material in a batch draws through the same one. The material
// itself is not part of the key: each drawable carries its own record slot.
type batch struct {
	pool       *materials.Pool
	cull       materials.CullMode
	blend      materials.BlendMode
	geometryID uint32

	// instanceCount sizes the batch's region in the visible buffer.
	instanceCount uint32
	// buildID is the id buildDrawables tagged this batch's drawables with, kept so
	// orderBatches can invert its own sort (see there).
	buildID uint32
	// poolRevision is the pool's rasterization revision when the batch was formed.
	// Comparing it against the pool's current one is how a material moving to a
	// different cull or blend mode is noticed — see isLayoutStale.
	poolRevision uint64
}

// hasRasterOf reports whether two batches share raster state, and so draw through the
// same pipeline.
func (b *batch) hasRasterOf(other *batch) bool {
	return b.pool == other.pool && b.cull == other.cull && b.blend == other.blend
}

// rasterSpans yields each span of adjacent batches sharing raster state, as its first
// batch and its length. orderBatches keeps such batches adjacent, so each span is
// exactly one multi-draw-indirect call: one pipeline, one material pool, many indirect
// commands.
func rasterSpans(batches []batch) iter.Seq2[int, int] {
	return func(yield func(first, count int) bool) {
		for first := 0; first < len(batches); {
			end := first + 1
			for end < len(batches) && batches[end].hasRasterOf(&batches[first]) {
				end++
			}
			if !yield(first, end-first) {
				return
			}
			first = end
		}
	}
}

// batchRange is the batches from first up to, but not including, end.
type batchRange struct {
	first, end int
}

// isEmpty reports whether the range holds no batches.
func (r batchRange) isEmpty() bool {
	return r.first == r.end
}

// splitByBlend returns the range of opaque batches and the range of blended batches
// after it. orderBatches puts every opaque batch first, so each is one contiguous run.
func splitByBlend(batches []batch) (opaque, transparent batchRange) {
	split := slices.IndexFunc(batches, func(b batch) bool {
		return b.blend != materials.BlendOpaque
	})
	if split < 0 {
		split = len(batches)
	}
	return batchRange{first: 0, end: split}, batchRange{first: split, end: len(batches)}
}

// isLayoutStale reports whether a layout still describes the scene the packet holds.
//
// Two things can invalidate it. The mesh table may have changed — objects added or
// removed, a material swapped — which the packet's revision reports. Or a material
// already in the layout may have changed cull or blend mode, which moves it to a
// different batch while leaving the mesh table untouched.
//
// The second is checked per POOL, not per material: cull and blend are the only raster
// state a material owns, and one counter per pool covers all of them (see
// materials.Pool.RasterRevision). Adjacent batches of a span share a pool and were
// formed at the same revision, so only the first of each needs checking — which keeps
// this proportional to the raster states in play rather than to objects on screen.
func isLayoutStale(p *scenes.FramePacket, layout *drawLayout) bool {
	if layout.meshRevision != p.Meshes.Revision {
		return true
	}
	for first := range rasterSpans(layout.batches) {
		b := &layout.batches[first]
		if b.pool.RasterRevision() != b.poolRevision {
			return true
		}
	}
	return false
}

// buildDrawables expands the packet's object-shaped mesh table into the flat draw
// records the cull pass works on — one per instance per LOD level — and groups them into
// batches as it goes.
//
// The two are done in one walk deliberately. A drawable's raster state is what decides
// its batch, so collecting the records and grouping them is the same question asked
// once; splitting it would mean carrying the answer in a slice parallel to the drawables
// purely to read it back a moment later.
//
// The iteration order is a contract, not an implementation detail: objects in table
// order, instances next, levels innermost. Anything reproducing this expansion has to
// agree, or records line up with the wrong materials.
func buildDrawables(p *scenes.FramePacket, layout *drawLayout, materialStore *materials.Store) {
	layout.drawables = layout.drawables[:0]
	layout.batches = layout.batches[:0]
	// Index 0 is reserved and never read: a drawable's lodID of 0 means "not LOD-tagged".
	layout.lods = append(layout.lods[:0], gpuLOD{})

	type key struct {
		pool     *materials.Pool
		cull     materials.CullMode
		blend    materials.BlendMode
		geometry uint32
	}
	batchOf := make(map[key]uint32)

	for _, mesh := range p.Meshes.Entries() {
		lodID := uint32(0)
		if mesh.LODRange.Count > 0 {
			lodID = uint32(len(layout.lods))
			layout.lods = append(layout.lods, lodConfig(mesh, p.LODs.Data))
		}

		var flags uint32
		if mesh.Flags&scenes.RenderCastsShadow != 0 {
			flags |= DrawableCastsShadow
		}
		if mesh.Flags&scenes.RenderReceivesShadow != 0 {
			flags |= DrawableReceivesShadow
		}

		for instance := range mesh.Transforms.Count {
			for level := uint32(0); level <= mesh.LODRange.Count; level++ {
				geometry, material := mesh.Geometry, mesh.Material
				if level > 0 {
					coarser := p.LODs.Data[mesh.LODRange.First+level-1]
					geometry, material = coarser.Geometry, coarser.Material
				}

				pool := materialStore.PoolAt(material.PoolID)
				k := key{
					pool:     pool,
					cull:     pool.CullAt(material.Slot),
					blend:    pool.BlendAt(material.Slot),
					geometry: geometry.Slot,
				}
				batchID, seen := batchOf[k]
				if !seen {
					batchID = uint32(len(layout.batches))
					batchOf[k] = batchID
					layout.batches = append(layout.batches, batch{
						pool:         k.pool,
						cull:         k.cull,
						blend:        k.blend,
						geometryID:   k.geometry,
						poolRevision: pool.RasterRevision(),
					})
				}
				layout.batches[batchID].instanceCount++

				layout.drawables = append(layout.drawables, gpuDrawable{
					bounds:      mesh.Bounds,
					transformID: mesh.Transforms.First + instance,
					geometryID:  geometry.Slot,
					materialID:  material.Slot,
					// Provisional: orderBatches remaps it once the batches are sorted.
					batchID:  batchID,
					flags:    flags,
					lodID:    lodID,
					lodLevel: level,
				})
			}
		}
	}
}

// lodConfig builds one mesh's LOD entry. boundaries[i] is where level i ends and level
// i+1 begins — which is level i+1's own MinDistance. The last level has no upper bound.
func lodConfig(mesh scenes.MeshPacket, levels []scenes.LODLevel) gpuLOD {
	lod := gpuLOD{hysteresis: mesh.LODHysteresis, levelCount: mesh.LODRange.Count + 1}
	for i := range min(mesh.LODRange.Count, uint32(len(lod.boundaries))) {
		lod.boundaries[i] = levels[mesh.LODRange.First+i].MinDistance
	}
	return lod
}

// orderBatches puts the batches in draw order and derives everything keyed to that
// order: each batch's region in the visible buffer, and the indirect template.
//
// Opaque batches come before blended ones, so blending composites over the opaque
// scene; within each group, batches sharing raster state are kept together, so they draw
// as a single multi-draw-indirect call.
func orderBatches(layout *drawLayout, geometryStore *geometries.Store) {
	// Sorted in place. Each batch first records the id its drawables were tagged with,
	// so the permutation can be inverted afterwards without sorting a separate index
	// array and then permuting the batches to match it.
	for i := range layout.batches {
		layout.batches[i].buildID = uint32(i)
	}
	sort.SliceStable(layout.batches, func(a, b int) bool {
		x, y := &layout.batches[a], &layout.batches[b]
		xOpaque, yOpaque := x.blend == materials.BlendOpaque, y.blend == materials.BlendOpaque
		if xOpaque != yOpaque {
			return xOpaque
		}
		if x.pool != y.pool {
			return x.pool.Index() < y.pool.Index()
		}
		if x.cull != y.cull {
			return x.cull < y.cull
		}
		return x.blend < y.blend
	})

	remap := make([]uint32, len(layout.batches))
	for id := range layout.batches {
		remap[layout.batches[id].buildID] = uint32(id)
	}
	for i := range layout.drawables {
		layout.drawables[i].batchID = remap[layout.drawables[i].batchID]
	}

	layout.template = layout.template[:0]
	var base uint32
	for i := range layout.batches {
		b := &layout.batches[i]
		layout.template = append(layout.template, indirectCmd{
			indexCount:    geometryStore.IndexCountAt(b.geometryID),
			firstIndex:    geometryStore.IndexBaseAt(b.geometryID),
			firstInstance: base,
		})
		base += (b.instanceCount + regionAlign - 1) &^ (regionAlign - 1)
	}

	layout.visibleCapacity = max(base, regionAlign)
}
