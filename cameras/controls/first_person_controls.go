package controls

import (
	"time"

	"github.com/bluescreen10/pix/glm"
	"github.com/bluescreen10/pix/input"
	"github.com/chewxy/math32"
)

// KeyMapping maps FirstPersonControls' fly-movement actions to keys.
type KeyMapping struct {
	Forward, Back, Left, Right, Up, Down input.Key
}

// DefaultKeyMapping is WASD to move, Space to rise, left Control to descend.
var DefaultKeyMapping = KeyMapping{
	Forward: input.KeyW,
	Back:    input.KeyS,
	Left:    input.KeyA,
	Right:   input.KeyD,
	Up:      input.KeySpace,
	Down:    input.KeyLeftControl,
}

// fpsInput is the input capability FirstPersonControls needs: mouse-look plus
// fly-movement keys. Declared here, by the consumer, rather than requiring a
// caller-specific concrete type — anything satisfying both input.MouseInput and
// input.KeyBoardInput (e.g. *glfwinput.Input) works for free.
type fpsInput interface {
	input.MouseInput
	input.KeyBoardInput
}

// FirstPersonControls is a mouse-look + WASD fly camera, the kind found in an FPS
// or a level editor's scene view: the mouse always steers yaw/pitch (no click-drag
// gate, unlike OrbitControls — the caller is expected to have captured/hidden the
// cursor), and Forward/Back/Left/Right move along the camera's horizontal facing so
// looking up or down doesn't send movement into the ground or sky; Up/Down move
// along world Y regardless of pitch.
type FirstPersonControls struct {
	camera camera
	input  fpsInput

	yaw, pitch float32

	moveSpeed float32 // units/second
	lookSpeed float32 // radians/pixel

	keyMapping KeyMapping

	mousePos   glm.Vec2f
	lastUpdate time.Time
}

// NewFirstPerson builds controls seeded from camera's current facing (so it doesn't
// snap on the first Update).
func NewFirstPerson(camera camera, in fpsInput) *FirstPersonControls {
	x, y := in.GetPos()
	fwd := camera.Fwd()

	return &FirstPersonControls{
		camera: camera,
		input:  in,

		yaw:   math32.Atan2(fwd.X(), fwd.Z()),
		pitch: math32.Asin(glm.Clamp(fwd.Y(), -1, 1)),

		moveSpeed: 5,
		lookSpeed: 0.0025,

		keyMapping: DefaultKeyMapping,

		mousePos:   glm.Vec2f{float32(x), float32(y)},
		lastUpdate: time.Now(),
	}
}

// SetMoveSpeed sets fly speed in units/second.
func (c *FirstPersonControls) SetMoveSpeed(speed float32) {
	c.moveSpeed = speed
}

// SetLookSpeed sets mouse sensitivity in radians/pixel.
func (c *FirstPersonControls) SetLookSpeed(speed float32) {
	c.lookSpeed = speed
}

// SetKeyMapping replaces the fly-movement key bindings.
func (c *FirstPersonControls) SetKeyMapping(mapping KeyMapping) {
	c.keyMapping = mapping
}

func (c *FirstPersonControls) Update() {
	now := time.Now()
	dt := now.Sub(c.lastUpdate).Seconds()
	c.lastUpdate = now

	x, y := c.input.GetPos()
	newPos := glm.Vec2f{float32(x), float32(y)}
	deltaMouse := newPos.Sub(c.mousePos)
	c.mousePos = newPos

	// Mouse-look: yaw increases turning right, pitch increases looking up. Screen Y
	// grows downward, so a downward mouse move (positive deltaY) should pitch down.
	c.yaw -= deltaMouse.X() * c.lookSpeed
	c.pitch -= deltaMouse.Y() * c.lookSpeed
	c.pitch = glm.Clamp(c.pitch, -1.5, 1.5)

	sinYaw, cosYaw := math32.Sincos(c.yaw)
	sinPitch, cosPitch := math32.Sincos(c.pitch)
	forward := glm.Vec3f{cosPitch * sinYaw, sinPitch, cosPitch * cosYaw}
	right := forward.Cross(glm.Vec3f{0, 1, 0}).Normalize()
	up := right.Cross(forward).Normalize()

	// Horizontal-only forward for movement, so looking up/down doesn't fly the
	// camera into the ground or sky. right is already horizontal: it's
	// perpendicular to world Y by construction.
	moveForward := glm.Vec3f{sinYaw, 0, cosYaw}

	pos := c.camera.Position()
	move := float32(dt) * c.moveSpeed
	if c.input.GetKey(c.keyMapping.Forward) == input.KeyPress {
		pos = pos.Add(moveForward.Scale(move))
	}
	if c.input.GetKey(c.keyMapping.Back) == input.KeyPress {
		pos = pos.Sub(moveForward.Scale(move))
	}
	if c.input.GetKey(c.keyMapping.Right) == input.KeyPress {
		pos = pos.Add(right.Scale(move))
	}
	if c.input.GetKey(c.keyMapping.Left) == input.KeyPress {
		pos = pos.Sub(right.Scale(move))
	}
	if c.input.GetKey(c.keyMapping.Up) == input.KeyPress {
		pos = pos.Add(glm.Vec3f{0, move, 0})
	}
	if c.input.GetKey(c.keyMapping.Down) == input.KeyPress {
		pos = pos.Sub(glm.Vec3f{0, move, 0})
	}

	c.camera.SetPosition(pos)
	c.camera.SetFwd(forward)
	c.camera.SetUp(up)
}
