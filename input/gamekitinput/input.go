// Package gamekitinput adapts GameKit's ui.Input — a window from any ui backend — to
// Pix's input interfaces, and a ui.PointerInput — a window, or a canvas within one —
// to Pix's mouse input.
package gamekitinput

import (
	"github.com/bluescreen10/gamekit/keyboard"
	"github.com/bluescreen10/gamekit/pointer"
	"github.com/bluescreen10/gamekit/ui"
	"github.com/bluescreen10/pix/input"
)

var _ input.MouseInput = Pointer{}
var _ input.MouseInput = (*Input)(nil)
var _ input.KeyboardInput = (*Input)(nil)
var _ input.TextInput = (*Input)(nil)
var _ input.KeyEvents = (*Input)(nil)

// Pointer reads the mouse from one surface. Over a canvas, it sees only the canvas's
// own presses and scrolling — not those aimed at the controls beside it.
type Pointer struct {
	source ui.PointerInput
}

// NewPointer reads the mouse from source: a ui.Window, or a ui.Canvas.
func NewPointer(source ui.PointerInput) Pointer {
	return Pointer{source: source}
}

func (p Pointer) Pos() (x, y float64) {
	return p.source.PointerPosition()
}

func (p Pointer) Scroll() (x, y float64) {
	return p.source.ScrollOffset()
}

func (p Pointer) Button(button input.MouseButton) input.MouseButtonAction {
	return input.MouseButtonAction(p.source.PointerButtonState(pointer.Button(button)))
}

// Input reads a window's keyboard, text and mouse.
type Input struct {
	Pointer
	source ui.Input

	// Buffered by the source's handlers and drained by Chars/Keys. No mutex: a ui
	// backend dispatches events on the thread calling PollEvents, which is the same
	// thread that drains them.
	chars []rune
	keys  []input.KeyEvent

	unsubscribeText ui.Unsubscribe
	unsubscribeKey  ui.Unsubscribe
}

// New reads input from source, usually a ui.Window. Close stops it listening.
func New(source ui.Input) *Input {
	in := &Input{
		Pointer: NewPointer(source),
		source:  source,
	}
	in.unsubscribeText = source.OnText(in.bufferChar)
	in.unsubscribeKey = source.OnKey(in.bufferKey)
	return in
}

// Close stops buffering the source's text and key events.
func (i *Input) Close() {
	i.unsubscribeText()
	i.unsubscribeKey()
}

func (i *Input) Key(key input.Key) input.KeyAction {
	return input.KeyAction(i.source.KeyState(keyboard.Key(key)))
}

// DisablePointer hides the pointer and locks it to the window, for mouse-look.
func (i *Input) DisablePointer() {
	i.source.SetPointerMode(pointer.Disabled)
}

// EnablePointer shows the pointer again and lets it leave the window.
func (i *Input) EnablePointer() {
	i.source.SetPointerMode(pointer.Normal)
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

func (i *Input) bufferChar(char rune) {
	i.chars = append(i.chars, char)
}

func (i *Input) bufferKey(event ui.KeyEvent) {
	i.keys = append(i.keys, input.KeyEvent{
		Key:    input.Key(event.Key),
		Action: input.KeyAction(event.Action),
		Mods:   input.ModifierKey(event.Modifiers),
	})
}
