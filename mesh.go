package pix

import (
	"github.com/bluescreen10/pix/geometries"
	"github.com/bluescreen10/pix/glm"
	"github.com/bluescreen10/pix/materials"
)

// Mesh is a typed node handle for a renderable mesh (geometry + material at a node).
// It embeds Node, so all hierarchy and transform methods are available directly.
type Mesh struct{ Node }

// meshData is the per-mesh payload stored in Scene.meshes. It holds ref-counted
// handles to renderer-owned resources plus the cached local bounds.
type meshData struct {
	geometry  geometries.Geometry
	material  materials.Material
	bounds    glm.Sphere
	ownerNode uint32
}

func (m Mesh) data() *meshData {
	return &m.scene.meshes[m.scene.payload[m.slot()]]
}

// Geometry returns the mesh's geometry handle.
func (m Mesh) Geometry() geometries.Geometry { return m.data().geometry }

// Material returns the mesh's material handle.
func (m Mesh) Material() materials.Material { return m.data().material }

// SetMaterial swaps the mesh's material (the cached materialID changes, so the
// scene's drawables are rebuilt).
func (m Mesh) SetMaterial(mat materials.Material) {
	md := m.data()
	newRef := mat.Copy()
	md.material.Release()
	md.material = newRef
	m.scene.drawableDirty = true
}

// BoundingSphere returns the mesh's local bounding sphere.
func (m Mesh) BoundingSphere() glm.Sphere { return m.data().bounds }

// NewMesh creates a mesh node from a geometry + material (both renderer-owned). The
// scene takes its own references (Copy), so the caller may Release theirs.
func (s *Scene) NewMesh(geo geometries.Geometry, mat materials.Material) Mesh {
	id := s.allocNode(KindMesh)
	payloadIdx := uint32(len(s.meshes))
	s.meshes = append(s.meshes, meshData{
		geometry:  geo.Copy(),
		material:  mat.Copy(),
		bounds:    geo.BoundingSphere(),
		ownerNode: id.index,
	})
	s.payload[id.index] = payloadIdx
	s.drawableDirty = true
	return Mesh{Node{scene: s, id: id}}
}
