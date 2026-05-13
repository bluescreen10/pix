package pix

// Mesh is a typed node handle for renderable mesh nodes.
// It embeds Node so all hierarchy and transform methods are available directly.
type Mesh struct{ Node }

// MeshData is the per-mesh payload stored in Scene.meshes.
type meshData struct {
	geometry  *GeometryData
	material  *MaterialData
	ownerNode uint32
}

func (m Mesh) data() *meshData {
	return &m.scene.meshes[m.scene.payload[m.slot()]]
}

func (m Mesh) Material() *MaterialData {
	return m.data().material
}

func (m Mesh) SetMaterial(mat *MaterialData) {
	m.data().material = mat
}

func (m Mesh) Geometry() *GeometryData {
	return m.data().geometry
}

func (m Mesh) SetGeometry(geo *GeometryData) {
	m.data().geometry = geo
}

// BoundingSphere returns the world-space bounding sphere, computed on demand.
func (m Mesh) BoundingSphere() Sphere {
	md := m.data()
	return transformSphere(md.geometry.BoundingSphere(), m.scene.world[m.slot()])
}

func (s *Scene) NewMesh(geometry *GeometryData, material *MaterialData) Mesh {
	id := s.allocNode(KindMesh)
	payloadIdx := uint32(len(s.meshes))
	s.meshes = append(s.meshes, meshData{
		geometry:  geometry,
		material:  material,
		ownerNode: id.index,
	})
	s.payload[id.index] = payloadIdx
	return Mesh{Node{scene: s, id: id}}
}
