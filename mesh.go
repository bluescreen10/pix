package pix

type Mesh struct {
	node
	geometry *GeometryData
	material *MaterialData
}

func NewMesh(geometry *GeometryData, material *MaterialData) *Mesh {
	return &Mesh{
		geometry: geometry,
		material: material,
		node:     newNode(),
	}
}
