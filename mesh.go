package pix

import "github.com/bluescreen10/pix/glm"

type Mesh struct {
	node
	model    glm.Mat4f
	geometry *Geometry
}

func NewMesh(geometry *Geometry) *Mesh {
	return &Mesh{
		geometry: geometry,
		model:    glm.Mat4Identity[float32](),
	}
}
