package pix

import "github.com/bluescreen10/pix/glm"

type renderable struct {
	geometry Geometry
	material Material
	model    glm.Mat4f
}
