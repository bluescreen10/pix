package pix

import (
	"github.com/bluescreen10/pix/geometries"
	"github.com/bluescreen10/pix/glm"
	"github.com/bluescreen10/pix/materials"
)

// maxLODLevels is the most levels a single LOD group (Mesh, InstancedMesh, or later
// SkinnedMesh) can have — bounded by gpuLODEntry's fixed-size boundaries array.
const maxLODLevels = 4

// lodLevel is one entry in a LOD chain: geometry/material shown once the camera is
// farther than minDistance. Level 0's minDistance is always 0 (implicit — never
// set explicitly, the zero value is already correct) since it's the closest level;
// there is no separate "end" distance for a level — it runs until the next level's
// own minDistance takes over, and the last level has no upper bound at all.
type lodLevel struct {
	geometry    geometries.Geometry
	material    materials.Material
	minDistance float32
}

// Mesh is a typed node handle for a renderable mesh (geometry + material at a node).
// It embeds Node, so all hierarchy and transform methods are available directly.
type Mesh struct{ Node }

// meshData is the per-mesh payload stored in Scene.meshes. lods[0] is always the mesh
// created by NewMesh; AddLOD appends coarser levels. bounds is shared by every level
// (see AddLOD's doc comment). lodGroupID is 0 until AddLOD is first called — it then
// indexes Scene.lodEntries, shared by every one of this mesh's level records.
type meshData struct {
	lods       []lodLevel
	hysteresis float32
	lodGroupID uint32
	bounds     glm.Sphere
	ownerNode  uint32
}

func (m Mesh) data() *meshData {
	return &m.scene.meshes[m.scene.payload[m.slot()]]
}

// Geometry returns the mesh's (level-0) geometry handle.
func (m Mesh) Geometry() geometries.Geometry { return m.data().lods[0].geometry }

// Material returns the mesh's (level-0) material handle.
func (m Mesh) Material() materials.Material { return m.data().lods[0].material }

// SetMaterial swaps the mesh's level-0 material (the cached materialID changes, so the
// scene's drawables are rebuilt). Coarser LOD levels added via AddLOD keep their own
// materials, untouched by this call.
func (m Mesh) SetMaterial(mat materials.Material) {
	md := m.data()
	newRef := mat.Copy()
	md.lods[0].material.Release()
	md.lods[0].material = newRef
	m.scene.drawableDirty = true
}

// BoundingSphere returns the mesh's local bounding sphere.
func (m Mesh) BoundingSphere() glm.Sphere { return m.data().bounds }

// AddLOD appends a coarser level, shown once the camera is farther than
// minDistance from the mesh (replacing whichever level was previously shown at
// that range, which now implicitly ends there). Levels must be added in increasing
// minDistance order (panics otherwise) and there are at most maxLODLevels total,
// counting the base level from NewMesh.
//
// Every level shares the base level's bounding sphere and node/transform — only
// geometry and material vary per level. A coarser level's own geometry extent is
// assumed to fit within the base level's bounds (true for the simplified-mesh /
// billboard-impostor case this is meant for); a level that's actually larger risks
// being frustum-culled early. See the LOD spec (project memory) for the full design.
func (m Mesh) AddLOD(geo geometries.Geometry, mat materials.Material, minDistance float32) Mesh {
	md := m.data()
	if len(md.lods) >= maxLODLevels {
		panic("pix: Mesh.AddLOD: at most maxLODLevels levels are supported")
	}
	if minDistance <= md.lods[len(md.lods)-1].minDistance {
		panic("pix: Mesh.AddLOD levels must be added in increasing minDistance order")
	}
	md.lods = append(md.lods, lodLevel{geometry: geo.Copy(), material: mat.Copy(), minDistance: minDistance})
	m.scene.rebuildLODEntry(&md.lodGroupID, md.lods, md.hysteresis)
	m.scene.drawableDirty = true
	return m
}

// SetLODHysteresis sets the sticky band (world units) used to resist flip-flopping
// between adjacent levels near a threshold. Default 0 (no hysteresis). Has no effect
// until at least one AddLOD call.
func (m Mesh) SetLODHysteresis(h float32) Mesh {
	md := m.data()
	md.hysteresis = h
	if len(md.lods) > 1 {
		m.scene.rebuildLODEntry(&md.lodGroupID, md.lods, md.hysteresis)
	}
	return m
}

// NewMesh creates a mesh node from a geometry + material (both renderer-owned). The
// scene takes its own references (Copy), so the caller may Release theirs.
func (s *Scene) NewMesh(geo geometries.Geometry, mat materials.Material) Mesh {
	id := s.allocNode(KindMesh)
	payloadIdx := uint32(len(s.meshes))
	s.meshes = append(s.meshes, meshData{
		lods:      []lodLevel{{geometry: geo.Copy(), material: mat.Copy()}},
		bounds:    geo.BoundingSphere(),
		ownerNode: id.index,
	})
	s.payload[id.index] = payloadIdx
	s.drawableDirty = true
	return Mesh{Node{scene: s, id: id}}
}
