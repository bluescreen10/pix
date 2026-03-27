package controls

import (
	"time"

	"github.com/bluescreen10/pix/glm"
	"github.com/go-gl/glfw/v3.3/glfw"
)

type camera interface {
	Rotate(roll, pitch, yaw float32)
	Move(x, y, z float32)
	Fwd() glm.Vec3f
	Up() glm.Vec3f
}

type OrbitControls struct {
	camera     camera
	input      *glfw.Window
	speed      float32
	keyMapping KeyMapping

	mousePos glm.Vec2f
}

type KeyMapping struct {
	Up    glfw.Key
	Down  glfw.Key
	Left  glfw.Key
	Right glfw.Key
}

// TODO: input should be an interface
func NewOrbit(camera camera, input *glfw.Window) *OrbitControls {
	return &OrbitControls{
		camera: camera,
		input:  input,
		speed:  1,
		keyMapping: KeyMapping{
			Up:    glfw.KeyUp,
			Down:  glfw.KeyDown,
			Left:  glfw.KeyLeft,
			Right: glfw.KeyRight,
		},
	}
}

func (c *OrbitControls) Update(delta time.Duration) {
	deltaMs := float32(delta.Milliseconds()) * 0.001
	speed := c.speed * deltaMs
	if c.input.GetKey(c.keyMapping.Up) != glfw.Release {
		d := c.camera.Fwd().Scale(speed)
		c.camera.Move(d[0], d[1], d[2])
	} else if c.input.GetKey(c.keyMapping.Down) != glfw.Release {
		d := c.camera.Fwd().Scale(-speed)
		c.camera.Move(d[0], d[1], d[2])
	} else if c.input.GetKey(c.keyMapping.Right) != glfw.Release {
		right := c.camera.Fwd().Cross(c.camera.Up()).Normalize()
		d := right.Scale(speed)
		c.camera.Move(d[0], d[1], d[2])
	} else if c.input.GetKey(c.keyMapping.Left) != glfw.Release {
		right := c.camera.Fwd().Cross(c.camera.Up()).Normalize()
		d := right.Scale(-speed)
		c.camera.Move(d[0], d[1], d[2])
	}

	x, y := c.input.GetCursorPos()
	newPos := glm.Vec2f{float32(x), float32(y)}
	d := newPos.Sub(c.mousePos).Scale(deltaMs)
	c.mousePos = newPos
	c.camera.Rotate(0, -d[1], -d[0])
}
