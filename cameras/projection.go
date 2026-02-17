package cameras

import "github.com/bluescreen10/pix/glm"

type PerspectiveCamera struct {
	position    glm.Vec3f
	rotation    glm.Vec3f
	target      glm.Vec3f
	up          glm.Vec3f
	aspectRatio float32
	nearZ       float32
	farZ        float32
	fov         float32
}

func NewPerpectiveCamera(fov float32, aspectRatio float32, nearZ float32, farZ float32) *PerspectiveCamera {
	return &PerspectiveCamera{
		fov:         fov,
		aspectRatio: aspectRatio,
		nearZ:       nearZ,
		farZ:        farZ,
		target:      glm.Vec3f{0, 0, 1},
		up:          glm.Vec3f{0, 1, 0},
	}
}

func (c *PerspectiveCamera) ViewProjection() glm.Mat4f {
	target := c.target.Add(c.position)
	view := glm.LookAtRH(c.position, target, c.up)
	projection := glm.PerspectiveRH(c.fov, c.aspectRatio, c.nearZ, c.farZ)
	return projection.Mul4x4(view)
}
