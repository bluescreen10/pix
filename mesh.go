package pix

type Mesh struct {
	node
	geometry *GeometryData
	material *MaterialData

	isBoundSphereValid bool
	boundingSphere     Sphere
}

func NewMesh(geometry *GeometryData, material *MaterialData) *Mesh {
	return &Mesh{
		geometry: geometry,
		material: material,
		node:     newNode(),
	}
}

func (m *Mesh) BoundingSphere() Sphere {
	if !m.isBoundSphereValid {
		m.isBoundSphereValid = true
		m.boundingSphere = transformSphere(m.geometry.BoundingSphere(), m.worldModel)
	}
	return m.boundingSphere
}

func (m *Mesh) NeedsUpdate() {
	m.isBoundSphereValid = false
}

func (m *Mesh) UpdateMatrix(force bool) bool {
	updated := m.node.UpdateMatrix(force)

	// if the world matrix updated invalidate bounding sphere
	if updated {
		m.isBoundSphereValid = false
	}

	return updated
}
