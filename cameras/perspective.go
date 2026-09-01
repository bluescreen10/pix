package cameras

import "github.com/bluescreen10/pix/glm"

// PerspectiveCamera is a target-based perspective camera: it looks from Position at
// Target, so moving the position keeps it aimed at the same point (orbit cameras just
// update the position each frame). FOV is in degrees.
type PerspectiveCamera struct {
	position glm.Vec3f
	target   glm.Vec3f
	up       glm.Vec3f
	fov      float32 // vertical field of view, degrees
	aspect   float32 // width / height
	near     float32
	far      float32
}

// NewPerspectiveCamera returns a camera at the origin looking down -Z (target at the
// origin). fov is in degrees.
func NewPerspectiveCamera(fov, aspect, near, far float32) *PerspectiveCamera {
	return &PerspectiveCamera{
		fov:    fov,
		aspect: aspect,
		near:   near,
		far:    far,
		up:     glm.Vec3f{0, 1, 0},
	}
}

func (c *PerspectiveCamera) Position() glm.Vec3f {
	return c.position
}

func (c *PerspectiveCamera) SetPosition(p glm.Vec3f) {
	c.position = p
}

func (c *PerspectiveCamera) Target() glm.Vec3f {
	return c.target
}

// SetTarget aims the camera at t (world space). LookAt is an alias.
func (c *PerspectiveCamera) SetTarget(t glm.Vec3f) {
	c.target = t
}

func (c *PerspectiveCamera) LookAt(t glm.Vec3f) {
	c.target = t
}

func (c *PerspectiveCamera) Up() glm.Vec3f {
	return c.up
}

func (c *PerspectiveCamera) SetUp(up glm.Vec3f) {
	c.up = up
}

// Fwd returns the normalized view direction (toward the target).
func (c *PerspectiveCamera) Fwd() glm.Vec3f {
	return c.target.Sub(c.position).Normalize()
}

// SetFwd aims the camera along dir from its current position (target = position + dir).
// This lets controllers that think in forward vectors (e.g. OrbitControls) drive the
// same target-based camera.
func (c *PerspectiveCamera) SetFwd(dir glm.Vec3f) {
	c.target = c.position.Add(dir)
}

func (c *PerspectiveCamera) SetFOV(fov float32) {
	c.fov = fov
}

func (c *PerspectiveCamera) SetAspect(a float32) {
	c.aspect = a
}

func (c *PerspectiveCamera) SetNear(n float32) {
	c.near = n
}

func (c *PerspectiveCamera) SetFar(f float32) {
	c.far = f
}

// ViewProjection returns the combined world→clip matrix. It re-runs LookAt each call,
// so a moved position stays aimed at Target.
func (c *PerspectiveCamera) ViewProjection() glm.Mat4f {
	view := glm.LookAtRH(c.position, c.target, c.up)
	proj := glm.PerspectiveRH(glm.ToRadians(c.fov), c.aspect, c.near, c.far)
	return proj.Mul4x4(view)
}
