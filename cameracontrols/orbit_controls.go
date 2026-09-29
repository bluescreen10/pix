package cameracontrols

import (
	"time"

	"github.com/bluescreen10/pix/glm"
	"github.com/bluescreen10/pix/input"
	"github.com/chewxy/math32"
)

type camera interface {
	Position() glm.Vec3f
	SetPosition(glm.Vec3f)
	Forward() glm.Vec3f
	SetForward(glm.Vec3f)
	Up() glm.Vec3f
	SetUp(glm.Vec3f)
}

type OrbitControls struct {
	camera camera
	mouse  input.MouseInput

	// orbit target
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

	mouseButtons MouseButtonMapping

	// state
	isRotating bool
	isPanning  bool
	mousePos   glm.Vec2f
	scroll     glm.Vec2f
	lastUpdate time.Time
}

type MouseButtonMapping struct {
	Orbit input.MouseButton
	Pan   input.MouseButton
	Zoom  input.MouseButton
}

func NewOrbit(camera camera, mouse input.MouseInput) *OrbitControls {
	x, y := mouse.Pos()
	scrollX, scrollY := mouse.Scroll()

	return &OrbitControls{
		camera: camera,
		mouse:  mouse,

		target: glm.Vec3f{0, 0, 0},

		rotateSpeed: 0.005,
		zoomSpeed:   0.1,
		panSpeed:    0.05,

		dampingEnabled: true,
		dampingFactor:  5,

		mouseButtons: MouseButtonMapping{
			Orbit: input.MouseButtonLeft,
			Pan:   input.MouseButtonRight,
		},

		mousePos:   glm.Vec2f{float32(x), float32(y)},
		scroll:     glm.Vec2f{float32(scrollX), float32(scrollY)},
		lastUpdate: time.Now(),
	}
}

func (c *OrbitControls) SetTarget(target glm.Vec3f) {
	c.desiredTarget = target
}

// SetPitch sets the target pitch. Positive pitch looks down from above the target,
// negative from below; 0 is level. Clamped to ±1.5 rad while orbiting.
func (c *OrbitControls) SetPitch(pitch float32) {
	c.desiredPitch = pitch
}

func (c *OrbitControls) SetYaw(yaw float32) {
	c.desiredYaw = yaw
}

func (c *OrbitControls) Update() {
	dt := time.Since(c.lastUpdate)
	x, y := c.mouse.Pos()
	newPos := glm.Vec2f{float32(x), float32(y)}

	x, y = c.mouse.Scroll()
	newScroll := glm.Vec2f{float32(x), float32(y)}

	pos := c.camera.Position()
	offset := pos.Sub(c.target)
	radius := offset.Length()

	// orbit
	if c.mouse.Button(c.mouseButtons.Orbit) == input.ButtonPress {
		if !c.isRotating {
			c.mousePos = newPos
			c.isRotating = true
		} else {
			deltaMouse := newPos.Sub(c.mousePos)
			c.desiredYaw -= deltaMouse[0] * c.rotateSpeed
			c.desiredPitch += deltaMouse[1] * c.rotateSpeed
			c.desiredPitch = glm.Clamp(c.desiredPitch, -1.5, 1.5)
		}
	} else {
		c.isRotating = false
	}

	// pan
	if c.mouse.Button(c.mouseButtons.Pan) == input.ButtonPress {
		if !c.isPanning {
			c.mousePos = newPos
			c.isPanning = true
		} else {
			deltaMouse := newPos.Sub(c.mousePos)
			right := c.camera.Forward().Cross(glm.Vec3f{0, 1, 0}).Normalize()
			up := right.Cross(c.camera.Forward()).Normalize()
			panX := right.Scale(-deltaMouse[0] * c.panSpeed * radius * 0.1)
			panY := up.Scale(deltaMouse[1] * c.panSpeed * radius * 0.1)
			c.desiredTarget = c.desiredTarget.Add(panX).Add(panY)
		}
	} else {
		c.isPanning = false
	}

	// GameKit reports a running scroll total so multiple consumers can observe it.
	// Apply only the amount accumulated since this controller's previous update.
	scrollDelta := newScroll.Y() - c.scroll.Y()
	if scrollDelta != 0 {
		radius *= glm.Clamp(1-scrollDelta*c.zoomSpeed, 0.1, 100)
		offset = offset.Normalize().Scale(radius)
	}

	// update internal state
	c.mousePos = newPos
	c.scroll = newScroll
	c.lastUpdate = time.Now()

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

	// build rotation quaternion from yaw/pitch. Pitch is negated so the convention is
	// positive pitch = look from above (offset.y = radius·sin(pitch)).
	quatYaw := glm.NewQuat(c.yaw, glm.Vec3f{0, 1, 0})
	quatPitch := glm.NewQuat(-c.pitch, glm.Vec3f{1, 0, 0})
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
	c.camera.SetForward(forward)
	c.camera.SetUp(up)
}
