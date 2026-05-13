package pix

// Mesh is a typed node handle for renderable mesh nodes.
// It embeds Node so all hierarchy and transform methods are available directly.
type Mesh struct{ Node }

// meshData is the per-mesh payload stored in Scene.meshes.
type meshData struct {
	geometry       Geometry
	material       Material
	boundingSphere Sphere
	ownerNode      uint32
}

func (m Mesh) data() *meshData {
	return &m.scene.meshes[m.scene.payload[m.slot()]]
}

func (m Mesh) Geometry() Geometry {
	return m.data().geometry
}

func (m Mesh) Material() Material {
	return m.data().material
}

func (m Mesh) SetMaterial(mat Material) {
	md := m.data()
	newRef := mat.Copy()
	md.material.Release()
	md.material = newRef
}

// BoundingSphere returns the world-space bounding sphere from the pre-computed local bounds.
func (m Mesh) BoundingSphere() Sphere {
	return m.data().geometry.BoundingSphere()
}

func (s *Scene) NewMesh(geo Geometry, mat Material) Mesh {
	id := s.allocNode(KindMesh)
	payloadIdx := uint32(len(s.meshes))
	s.meshes = append(s.meshes, meshData{
		geometry:       geo.Copy(),
		material:       mat.Copy(),
		boundingSphere: geo.BoundingSphere(),
		ownerNode:      id.index,
	})
	s.payload[id.index] = payloadIdx
	return Mesh{Node{scene: s, id: id}}
}
