package cameracontrols_test

import (
	"testing"

	"github.com/bluescreen10/pix/cameracontrols"
	"github.com/bluescreen10/pix/glm"
	"github.com/bluescreen10/pix/input"
)

// fakeCamera stores what the controls set on it.
type fakeCamera struct {
	position, forward, up glm.Vec3f
}

func (c *fakeCamera) Position() glm.Vec3f {
	return c.position
}

func (c *fakeCamera) SetPosition(position glm.Vec3f) {
	c.position = position
}

func (c *fakeCamera) Forward() glm.Vec3f {
	return c.forward
}

func (c *fakeCamera) SetForward(forward glm.Vec3f) {
	c.forward = forward
}

func (c *fakeCamera) Up() glm.Vec3f {
	return c.up
}

func (c *fakeCamera) SetUp(up glm.Vec3f) {
	c.up = up
}

// fakeMouse is a pointer the test moves by hand, with no keys or buttons held.
type fakeMouse struct {
	x, y float64
}

func (m *fakeMouse) Pos() (x, y float64) {
	return m.x, m.y
}

func (m *fakeMouse) Button(button input.MouseButton) input.MouseButtonAction {
	return input.ButtonRelease
}

func (m *fakeMouse) Scroll() (x, y float64) {
	return 0, 0
}

func (m *fakeMouse) Key(key input.Key) input.KeyAction {
	return input.KeyRelease
}

func TestFirstPersonMouseLookDisabledIgnoresMouse(t *testing.T) {
	camera := &fakeCamera{forward: glm.Vec3f{0, 0, -1}, up: glm.Vec3f{0, 1, 0}}
	mouse := &fakeMouse{}
	controls := cameracontrols.NewFirstPerson(camera, mouse)
	controls.Update()
	want := camera.Forward()

	controls.EnableMouseLook(false)
	mouse.x, mouse.y = 300, 120
	controls.Update()
	if got := camera.Forward(); got != want {
		t.Errorf("Forward() after moving the mouse with mouse-look off = %v, want %v", got, want)
	}

	// The travel while it was off must not land on the first update back on.
	controls.EnableMouseLook(true)
	controls.Update()
	if got := camera.Forward(); got != want {
		t.Errorf("Forward() after turning mouse-look back on = %v, want %v", got, want)
	}

	mouse.x += 100
	controls.Update()
	if got := camera.Forward(); got == want {
		t.Errorf("Forward() after moving the mouse with mouse-look on = %v, want it to turn", got)
	}
}
