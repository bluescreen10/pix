package pix

import "github.com/bluescreen10/pix/glm"

type Mesh struct {
	node
	geometry *Geometry
	material *MaterialData
}

func NewMesh(geometry *Geometry, material *MaterialData) *Mesh {
	return &Mesh{
		geometry: geometry,
		material: material,
		node:     node{model: glm.Mat4Identity[float32]()},
	}
}
