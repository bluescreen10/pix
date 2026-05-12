package pix

import "github.com/bluescreen10/pix/glm"

type Scene struct {
	background glm.Color4f
	Object3D
}

func (s *Scene) SetBackground(color glm.Color4f) {
	s.background = color
}

type Group struct {
	Object3D
}

func NewScene() *Scene {
	return &Scene{Object3D: newObject3D()}
}

func newObject3D() Object3D {
	return Object3D{
		scale:         glm.Vec3f{1, 1, 1},
		rot:           glm.QuatIdentityf,
		localModel:    glm.Mat4fIndentity,
		worldModel:    glm.Mat4fIndentity,
		invWorldModel: glm.Mat4fIndentity,
	}
}
