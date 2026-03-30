package glfwinput

import (
	"github.com/bluescreen10/pix/input"
	"github.com/go-gl/glfw/v3.3/glfw"
)

var _ input.MouseInput = (*Input)(nil)
var _ input.KeyBoardInput = (*Input)(nil)

type Input struct {
	window *glfw.Window

	scrollX float64
	scrollY float64
}

func New(window *glfw.Window) *Input {
	input := &Input{
		window: window,
	}

	window.SetScrollCallback(input.scrollCallback)
	return input
}

func (i *Input) GetPos() (x, y float64) {
	return i.window.GetCursorPos()
}

func (i *Input) GetScroll() (x, y float64) {
	return i.scrollX, i.scrollY
}

func (i *Input) GetButton(button input.MouseButton) input.MouseButtonAction {
	state := i.window.GetMouseButton(glfw.MouseButton(button))
	return input.MouseButtonAction(state)
}

func (i *Input) GetKey(key input.Key) input.KeyAction {
	state := i.window.GetKey(glfw.Key(key))
	return input.KeyAction(state)
}

func (i *Input) scrollCallback(_ *glfw.Window, xoff, yoff float64) {
	i.scrollX = xoff
	i.scrollY = yoff
}
