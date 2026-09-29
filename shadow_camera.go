package pix

import "github.com/bluescreen10/pix/glm"

// perspectiveCamera and orthographicCamera are the renderer's own shadow cameras. They
// are not scene nodes: a fit aims them from a position at a target every frame, and
// nothing in the scene should be able to parent or move them.

type perspectiveCamera struct {
	position glm.Vec3f
	target   glm.Vec3f
	up       glm.Vec3f
	fov      float32 // vertical field of view, degrees
	aspect   float32 // width / height
	near     float32
	far      float32
}

func newPerspectiveCamera(fov, aspect, near, far float32) *perspectiveCamera {
	return &perspectiveCamera{
		fov:    fov,
		aspect: aspect,
		near:   near,
		far:    far,
		up:     glm.Vec3f{0, 1, 0},
	}
}

func (c *perspectiveCamera) Position() glm.Vec3f {
	return c.position
}

func (c *perspectiveCamera) SetPosition(position glm.Vec3f) {
	c.position = position
}

func (c *perspectiveCamera) SetTarget(target glm.Vec3f) {
	c.target = target
}

func (c *perspectiveCamera) SetUp(up glm.Vec3f) {
	c.up = up
}

func (c *perspectiveCamera) SetFOV(fov float32) {
	c.fov = fov
}

func (c *perspectiveCamera) SetFar(far float32) {
	c.far = far
}

func (c *perspectiveCamera) ViewProjection() glm.Mat4f {
	view := glm.LookAtRH(c.position, c.target, c.up)
	projection := glm.PerspectiveRevZRH(glm.ToRadians(c.fov), c.aspect, c.near, c.far)
	return projection.Mul4x4(view)
}

type orthographicCamera struct {
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

func newOrthographicCamera(left, right, bottom, top, near, far float32) *orthographicCamera {
	return &orthographicCamera{
		left:   left,
		right:  right,
		bottom: bottom,
		top:    top,
		near:   near,
		far:    far,
		up:     glm.Vec3f{0, 1, 0},
	}
}

func (c *orthographicCamera) Position() glm.Vec3f {
	return c.position
}

func (c *orthographicCamera) SetPosition(position glm.Vec3f) {
	c.position = position
}

func (c *orthographicCamera) SetTarget(target glm.Vec3f) {
	c.target = target
}

func (c *orthographicCamera) SetUp(up glm.Vec3f) {
	c.up = up
}

func (c *orthographicCamera) SetFrustum(left, right, bottom, top float32) {
	c.left, c.right, c.bottom, c.top = left, right, bottom, top
}

func (c *orthographicCamera) SetClip(near, far float32) {
	c.near, c.far = near, far
}

func (c *orthographicCamera) ViewProjection() glm.Mat4f {
	view := glm.LookAtRH(c.position, c.target, c.up)
	projection := glm.OrthoFullRevZRH(c.left, c.right, c.bottom, c.top, c.near, c.far)
	return projection.Mul4x4(view)
}
