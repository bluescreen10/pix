package controls

import (
	"time"

	"github.com/bluescreen10/pix/glm"
	"github.com/chewxy/math32"
	"github.com/go-gl/glfw/v3.3/glfw"
)

type camera interface {
	Position() glm.Vec3f
	SetPosition(glm.Vec3f)
	Fwd() glm.Vec3f
	SetFwd(glm.Vec3f)
	Up() glm.Vec3f
	SetUp(glm.Vec3f)
}

type OrbitControls struct {
	camera camera
	input  *glfw.Window

	// target Point
	target        glm.Vec3f
	desiredTarget glm.Vec3f

	// rotation
	yaw          float32
	pitch        float32
	desiredYaw   float32
	desiredPitch float32

	// speeds
	rotateSpeed float32
	zoomSpeed   float32
	panSpeed    float32

	// damping
	dampingEnabled bool
	dampingFactor  float32

	// mappings
	//keyMapping   KeyMapping
	mouseButtons MouseButtonMapping

	//temp
	scrollX float64
	scrollY float64

	// state
	isRotating bool
	isPanning  bool
	mousePos   glm.Vec2f
}

type MouseButtonMapping struct {
	Orbit glfw.MouseButton
	Pan   glfw.MouseButton
	Zoom  glfw.MouseButton
}

// TODO: input should be an interface
func NewOrbit(camera camera, input *glfw.Window) *OrbitControls {
	x, y := input.GetCursorPos()

	c := &OrbitControls{
		camera: camera,
		input:  input,

		target: glm.Vec3f{0, 0, 0},

		rotateSpeed: 0.005,
		zoomSpeed:   0.1,
		panSpeed:    0.05,

		dampingEnabled: true,
		dampingFactor:  5,

		mouseButtons: MouseButtonMapping{
			Orbit: glfw.MouseButtonLeft,
			Pan:   glfw.MouseButtonRight,
		},

		mousePos: glm.Vec2f{float32(x), float32(y)},
	}

	input.SetScrollCallback(c.ScrollCb)

	return c
}

func (c *OrbitControls) Update(dt time.Duration) {
	x, y := c.input.GetCursorPos()
	newPos := glm.Vec2f{float32(x), float32(y)}

	pos := c.camera.Position()
	offset := pos.Sub(c.target)
	radius := offset.Length()

	// orbit
	if c.input.GetMouseButton(c.mouseButtons.Orbit) == glfw.Press {
		if !c.isRotating {
			c.mousePos = newPos
			c.isRotating = true
		} else {
			deltaMouse := newPos.Sub(c.mousePos)
			c.desiredYaw -= deltaMouse[0] * c.rotateSpeed
			c.desiredPitch -= deltaMouse[1] * c.rotateSpeed
			c.desiredPitch = glm.Clamp(c.desiredPitch, -1.5, 1.5)
		}
	} else {
		c.isRotating = false
	}

	// pan
	if c.input.GetMouseButton(c.mouseButtons.Pan) == glfw.Press {
		if !c.isPanning {
			c.mousePos = newPos
			c.isPanning = true
		} else {
			deltaMouse := newPos.Sub(c.mousePos)
			right := c.camera.Fwd().Cross(glm.Vec3f{0, 1, 0}).Normalize()
			up := right.Cross(c.camera.Fwd()).Normalize()
			panX := right.Scale(-deltaMouse[0] * c.panSpeed * radius * 0.1)
			panY := up.Scale(deltaMouse[1] * c.panSpeed * radius * 0.1)
			c.desiredTarget = c.desiredTarget.Add(panX).Add(panY)
		}
	} else {
		c.isPanning = false
	}

	// update mouse position
	c.mousePos = newPos

	// zoom
	if c.scrollY != 0 {
		radius *= glm.Clamp(1-float32(c.scrollY)*c.zoomSpeed, 0.1, 100)
		c.scrollY = 0
		offset = offset.Normalize().Scale(radius)
	}

	// damping
	if c.dampingEnabled {
		lerpFactor := 1.0 - math32.Exp(-c.dampingFactor*float32(dt.Seconds()))
		c.yaw += (c.desiredYaw - c.yaw) * lerpFactor
		c.pitch += (c.desiredPitch - c.pitch) * lerpFactor
		c.target = c.target.Add(c.desiredTarget.Sub(c.target).Scale(lerpFactor))
	} else {
		c.yaw = c.desiredYaw
		c.pitch = c.desiredPitch
		c.target = c.desiredTarget
	}

	// build rotation quaternion from yaw/pitch
	quatYaw := glm.NewQuat(c.yaw, glm.Vec3f{0, 1, 0})
	quatPitch := glm.NewQuat(c.pitch, glm.Vec3f{1, 0, 0})
	rot := quatYaw.Mul(quatPitch)

	// rebuild offset from base vector
	base := glm.Vec3f{0, 0, radius}
	offset = rot.Rotate(base)
	pos = c.target.Add(offset)
	forward := c.target.Sub(pos).Normalize()
	right := forward.Cross(glm.Vec3f{0, 1, 0}).Normalize()
	up := right.Cross(forward).Normalize()

	// update camera position and orientation
	c.camera.SetPosition(pos)
	c.camera.SetFwd(forward)
	c.camera.SetUp(up)
}

func (c *OrbitControls) ScrollCb(_ *glfw.Window, xoff, yoff float64) {
	c.scrollX = xoff
	c.scrollY = yoff
}
