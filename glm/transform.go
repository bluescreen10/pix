package glm

import (
	"github.com/chewxy/math32"
)

// rotation order yaw (Z) -> pitch (Y) -> roll (X)

type Transform struct {
	pos   Vec3f
	rot   Vec3f
	scale Vec3f
}

func (t *Transform) SetPosition(x, y, z float32) {
	t.pos[0] = x
	t.pos[1] = y
	t.pos[2] = z
}

func (t *Transform) SetRotation(roll, pitch, yaw float32) {
	t.rot[0] = roll
	t.rot[1] = pitch
	t.rot[2] = yaw
}

func (t *Transform) SetScale(x, y, z float32) {
	t.scale[0] = x
	t.scale[1] = y
	t.scale[2] = z
}

func (t *Transform) SetUniformScale(s float32) {
	t.scale[0] = s
	t.scale[1] = s
	t.scale[2] = s
}

func (t *Transform) Rotate(roll, pitch, yaw float32) {
	t.rot[0] += roll
	t.rot[1] += pitch
	t.rot[2] += yaw
}

func (t *Transform) Move(x, y, z float32) {
	t.pos[0] += x
	t.pos[1] += y
	t.pos[2] += z
}

func (t *Transform) GetMatrix() Mat4f {
	sinPhi, cosPhi := math32.Sincos(t.rot[0])
	sinTheta, cosTheta := math32.Sincos(t.rot[1])
	sinPsi, cosPsi := math32.Sincos(t.rot[2])

	return Mat4f{
		// Column 1 (X-axis basis)
		t.scale[0] * cosPsi * cosTheta,
		t.scale[0] * sinPsi * cosTheta,
		t.scale[0] * (-sinTheta),
		0,

		// Column 2 (Y-axis basis)
		t.scale[1] * (cosPsi*sinTheta*sinPhi - sinPsi*cosPhi),
		t.scale[1] * (sinPsi*sinTheta*sinPhi + cosPsi*cosPhi),
		t.scale[1] * (cosTheta * sinPhi),
		0,

		// Column 3 (Z-axis basis)
		t.scale[2] * (cosPsi*sinTheta*cosPhi + sinPsi*sinPhi),
		t.scale[2] * (sinPsi*sinTheta*cosPhi - cosPsi*sinPhi),
		t.scale[2] * cosTheta * cosPhi,
		0,

		// Column 4 (Translation / Position)
		t.pos[0],
		t.pos[1],
		t.pos[2],
		1,
	}
}

func NewTransform() *Transform {
	return &Transform{scale: Vec3f{1, 1, 1}}
}
