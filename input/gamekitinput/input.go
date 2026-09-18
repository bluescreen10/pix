// Package gamekitinput adapts a GameKit window to Pix's input interfaces.
package gamekitinput

import (
	"github.com/bluescreen10/gamekit"
	"github.com/bluescreen10/gamekit/keyboard"
	"github.com/bluescreen10/gamekit/pointer"
	"github.com/bluescreen10/pix/input"
)

var _ input.MouseInput = (*Input)(nil)
var _ input.KeyboardInput = (*Input)(nil)
var _ input.TextInput = (*Input)(nil)
var _ input.KeyEvents = (*Input)(nil)

type Input struct {
	window *gamekit.Window

	// Buffered by GameKit callbacks and drained by Chars/Keys. No mutex: GameKit invokes
	// callbacks on the thread calling PollEvents, which is the same thread that
	// drains them.
	chars []rune
	keys  []input.KeyEvent
}

func New(window *gamekit.Window) *Input {
	in := &Input{
		window: window,
	}

	window.SetCharCallback(in.charCallback)
	window.SetKeyCallback(in.keyCallback)
	return in
}

func (i *Input) Pos() (x, y float64) {
	return i.window.GetPointerPos()
}

func (i *Input) Scroll() (x, y float64) {
	return i.window.GetScroll()
}

func (i *Input) Button(button input.MouseButton) input.MouseButtonAction {
	return input.MouseButtonAction(i.window.GetPointerButton(pointer.Button(button)))
}

func (i *Input) Key(key input.Key) input.KeyAction {
	return input.KeyAction(i.window.GetKey(keyboard.Key(key)))
}

func (i *Input) DisablePointer() {
	i.window.DisablePointer()
}

func (i *Input) EnablePointer() {
	i.window.EnablePointer()
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

func (i *Input) charCallback(_ *gamekit.Window, char rune) {
	i.chars = append(i.chars, char)
}

func (i *Input) keyCallback(_ *gamekit.Window, key keyboard.Key, _ int, action keyboard.KeyAction, mods keyboard.ModifierKey) {
	i.keys = append(i.keys, input.KeyEvent{
		Key:    input.Key(key),
		Action: input.KeyAction(action),
		Mods:   input.ModifierKey(mods),
	})
}
