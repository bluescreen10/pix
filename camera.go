package pix

import "github.com/bluescreen10/pix/glm"

// Camera is a right-handed perspective camera. It is passed to Renderer.Render and
// is not stored on the Scene, so one Scene can be viewed from many cameras.
type Camera struct {
	Position glm.Vec3f
	Target   glm.Vec3f
	Up       glm.Vec3f
	FOV      float32 // vertical field of view, degrees
	Aspect   float32 // width / height
	Near     float32
	Far      float32
}

// NewCamera returns a perspective camera with sensible defaults (looking down -Z).
func NewCamera() *Camera {
	return &Camera{
		Up:     glm.Vec3f{0, 1, 0},
		FOV:    45,
		Aspect: 1,
		Near:   0.1,
		Far:    1000,
	}
}

// View returns the world→view matrix.
func (c *Camera) View() glm.Mat4f { return glm.LookAtRH(c.Position, c.Target, c.Up) }

// Proj returns the view→clip perspective matrix.
func (c *Camera) Proj() glm.Mat4f {
	return glm.PerspectiveRH[float32](glm.ToRadians(c.FOV), c.Aspect, c.Near, c.Far)
}

// ViewProj returns the combined world→clip matrix.
func (c *Camera) ViewProj() glm.Mat4f { return c.Proj().Mul4x4(c.View()) }
