package cameras

import "github.com/bluescreen10/pix/glm"

type OrthographicCamera struct {
	position glm.Vec3f
	target   glm.Vec3f
	up       glm.Vec3f
	left     float32
	right    float32
	bottom   float32
	top      float32
	near     float32
	far      float32
}

func NewOrthographicCamera(left, right, bottom, top, near, far float32) *OrthographicCamera {
	return &OrthographicCamera{
		left:   left,
		right:  right,
		bottom: bottom,
		top:    top,
		near:   near,
		far:    far,
		up:     glm.Vec3f{0, 1, 0},
	}
}

func (c *OrthographicCamera) Position() glm.Vec3f                              { return c.position }
func (c *OrthographicCamera) SetPosition(p glm.Vec3f)                          { c.position = p }
func (c *OrthographicCamera) SetTarget(t glm.Vec3f)                            { c.target = t }
func (c *OrthographicCamera) SetUp(up glm.Vec3f)                               { c.up = up }
func (c *OrthographicCamera) SetFrustum(left, right, bottom, top float32)      {
	c.left, c.right, c.bottom, c.top = left, right, bottom, top
}

func (c *OrthographicCamera) ViewProjection() glm.Mat4f {
	view := glm.LookAtRH(c.position, c.target, c.up)
	proj := glm.OrthoFullRH(c.left, c.right, c.bottom, c.top, c.near, c.far)
	return proj.Mul4x4(view)
}
