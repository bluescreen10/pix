package pix

import "github.com/bluescreen10/pix/glm"

type Camera interface {
	ViewProjection() glm.Mat4f
	Position() glm.Vec3f
}
