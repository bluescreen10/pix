package glfwinput

import (
	"github.com/bluescreen10/pix/input"
	"github.com/go-gl/glfw/v3.3/glfw"
)

var _ input.MouseInput = (*Input)(nil)
var _ input.KeyBoardInput = (*Input)(nil)
var _ input.TextInput = (*Input)(nil)
var _ input.KeyEvents = (*Input)(nil)

type Input struct {
	window *glfw.Window

	scrollX float64
	scrollY float64

	// Buffered by GLFW callbacks and drained by Chars/Keys. No mutex: GLFW invokes
	// callbacks on the thread calling PollEvents, which is the same thread that
	// drains them.
	chars []rune
	keys  []input.KeyEvent
}

func New(window *glfw.Window) *Input {
	input := &Input{
		window: window,
	}

	window.SetScrollCallback(input.scrollCallback)
	window.SetCharCallback(input.charCallback)
	window.SetKeyCallback(input.keyCallback)
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

func (i *Input) DisableCursor() {
	i.window.SetInputMode(glfw.CursorMode, glfw.CursorDisabled)
}

func (i *Input) EnableCursor() {
	i.window.SetInputMode(glfw.CursorMode, glfw.CursorNormal)
}

// Chars drains the characters typed since the last call.
func (i *Input) Chars() []rune {
	if len(i.chars) == 0 {
		return nil
	}
	out := make([]rune, len(i.chars))
	copy(out, i.chars)
	i.chars = i.chars[:0]
	return out
}

// Keys drains the key transitions since the last call.
func (i *Input) Keys() []input.KeyEvent {
	if len(i.keys) == 0 {
		return nil
	}
	out := make([]input.KeyEvent, len(i.keys))
	copy(out, i.keys)
	i.keys = i.keys[:0]
	return out
}

func (i *Input) scrollCallback(_ *glfw.Window, xoff, yoff float64) {
	i.scrollX = xoff
	i.scrollY = yoff
}

func (i *Input) charCallback(_ *glfw.Window, char rune) {
	i.chars = append(i.chars, char)
}

func (i *Input) keyCallback(_ *glfw.Window, key glfw.Key, _ int, action glfw.Action, mods glfw.ModifierKey) {
	i.keys = append(i.keys, input.KeyEvent{
		Key:    input.Key(key),
		Action: input.KeyAction(action),
		Mods:   input.ModifierKey(mods),
	})
}
