package scenes

import (
	"github.com/bluescreen10/pix/geometries"
	"github.com/bluescreen10/pix/glm"
	"github.com/bluescreen10/pix/materials"
)

// Mesh is a typed node handle for a renderable mesh (geometry + material at a node).
// It embeds Node, so all hierarchy and transform methods are available directly.
type Mesh struct{ Node }

// meshData is the per-mesh payload stored in Scene.meshes. lods[0] is always the mesh
// created by NewMesh; AddLOD appends coarser levels. bounds is shared by every level
// (see AddLOD's doc comment), and grows with the morph target weights.
//
// A mesh whose geometry has morph targets draws deformOutput instead of its geometry
// (see Geometry.CreateDeformOutput); for any other mesh deformOutput is the zero
// Geometry. meshPacketIndex is where rebuildPacketTables put the mesh in the packet's
// mesh table, or invalidIndex if it was left out.
type meshData struct {
	lods       []lodLevel
	hysteresis float32
	bounds     glm.Sphere
	ownerNode  uint32

	morph           morphState
	deformOutput    geometries.Geometry
	meshPacketIndex uint32
}

func (m Mesh) data() *meshData {
	return &m.scene.meshes[m.scene.payload[m.slot()]]
}

// Geometry returns the mesh's (level-0) geometry handle.
func (m Mesh) Geometry() geometries.Geometry {
	return m.data().lods[0].geometry
}

// Material returns the mesh's (level-0) material handle.
func (m Mesh) Material() materials.Material {
	return m.data().lods[0].material
}

// SetMaterial swaps the mesh's level-0 material (the cached materialID changes, so the
// scene's drawables are rebuilt). Coarser LOD levels added via AddLOD keep their own
// materials, untouched by this call.
func (m Mesh) SetMaterial(mat materials.Material) {
	md := m.data()
	newRef := mat.Copy()
	md.lods[0].material.Release()
	md.lods[0].material = newRef
	m.scene.packetDirty = true
}

// BoundingSphere returns the mesh's local bounding sphere.
func (m Mesh) BoundingSphere() glm.Sphere {
	return m.data().bounds
}

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
// being frustum-culled early.
//
// On a mesh with morph targets, a level made with CreateLOD from the mesh's geometry
// is morphed with it; any other level is drawn as it is, and must have no morph
// targets of its own (panics otherwise).
func (m Mesh) AddLOD(geo geometries.Geometry, mat materials.Material, minDistance float32) Mesh {
	md := m.data()
	md.lods = appendLODLevel(md.lods, geo, mat, minDistance, md.deformOutput)
	m.scene.packetDirty = true
	return m
}

// SetLODHysteresis sets the sticky band (world units) used to resist flip-flopping
// between adjacent levels near a threshold. Default 0 (no hysteresis). Has no effect
// until at least one AddLOD call.
func (m Mesh) SetLODHysteresis(h float32) Mesh {
	md := m.data()
	md.hysteresis = h
	if len(md.lods) > 1 {
		m.scene.packetDirty = true
	}
	return m
}

// NewMesh creates a mesh node from a geometry + material (both renderer-owned). The
// scene takes its own references (Copy), so the caller may Release theirs. A geometry
// with morph targets starts with every weight at 0.
func (s *Scene) NewMesh(geo geometries.Geometry, mat materials.Material) Mesh {
	id := s.allocNode(kindMesh)
	payloadIdx := uint32(len(s.meshes))
	s.meshes = append(s.meshes, meshData{
		lods:            []lodLevel{{geometry: geo.Copy(), material: mat.Copy()}},
		bounds:          geo.BoundingSphere(),
		ownerNode:       id.index,
		morph:           newMorphState(geo),
		deformOutput:    newMorphOutput(geo),
		meshPacketIndex: invalidIndex,
	})
	s.payload[id.index] = payloadIdx
	s.packetDirty = true
	return Mesh{Node{scene: s, id: id}}
}
