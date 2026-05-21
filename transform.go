package pix

import "github.com/bluescreen10/pix/glm"

// Transform holds the decomposed local transform components for a scene node.
type Transform struct {
	Position glm.Vec3f
	Rotation glm.Quatf
	Scale    glm.Vec3f
}

func (t Transform) Matrix() glm.Mat4f {
	return glm.Transform(t.Scale, t.Rotation, t.Position)
}

var defaultTransform = Transform{
	Rotation: glm.QuatIdentityf,
	Scale:    glm.Vec3f{1, 1, 1},
}
