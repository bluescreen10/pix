//go:build js

package util

import (
	"fmt"
	"sync"
	"syscall/js"

	"github.com/bluescreen10/pix/input"
)

// Input tracks mouse and keyboard state via DOM events on a canvas element.
type Input struct {
	mu      sync.Mutex
	mouseX  float64
	mouseY  float64
	buttons [3]input.MouseButtonAction
	scrollX float64
	scrollY float64
	keys    map[string]bool

	// held to prevent GC of the JS callback functions
	handlers []js.Func
}

func newInput(canvas js.Value) *Input {
	inp := &Input{keys: make(map[string]bool)}

	inp.listen(canvas, "mousemove", func(_ js.Value, args []js.Value) any {
		e := args[0]
		rect := canvas.Call("getBoundingClientRect")
		inp.mu.Lock()
		inp.mouseX = e.Get("clientX").Float() - rect.Get("left").Float()
		inp.mouseY = e.Get("clientY").Float() - rect.Get("top").Float()
		inp.mu.Unlock()
		return nil
	})

	inp.listen(canvas, "mousedown", func(_ js.Value, args []js.Value) any {
		b := args[0].Get("button").Int()
		inp.mu.Lock()
		if b < 3 {
			inp.buttons[b] = input.ButtonPress
		}
		inp.mu.Unlock()
		return nil
	})

	inp.listen(canvas, "mouseup", func(_ js.Value, args []js.Value) any {
		b := args[0].Get("button").Int()
		inp.mu.Lock()
		if b < 3 {
			inp.buttons[b] = input.ButtonRelease
		}
		inp.mu.Unlock()
		return nil
	})

	inp.listen(canvas, "wheel", func(_ js.Value, args []js.Value) any {
		e := args[0]
		inp.mu.Lock()
		inp.scrollX = e.Get("deltaX").Float()
		inp.scrollY = -e.Get("deltaY").Float() // flip: wheel-down is negative deltaY
		inp.mu.Unlock()
		return nil
	})

	doc := js.Global().Get("document")
	inp.listen(doc, "keydown", func(_ js.Value, args []js.Value) any {
		code := args[0].Get("code").String()
		inp.mu.Lock()
		inp.keys[code] = true
		inp.mu.Unlock()
		return nil
	})
	inp.listen(doc, "keyup", func(_ js.Value, args []js.Value) any {
		code := args[0].Get("code").String()
		inp.mu.Lock()
		delete(inp.keys, code)
		inp.mu.Unlock()
		return nil
	})

	return inp
}

func (i *Input) listen(target js.Value, event string, fn func(js.Value, []js.Value) any) {
	f := js.FuncOf(fn)
	i.handlers = append(i.handlers, f)
	target.Call("addEventListener", event, f)
}

func (i *Input) GetPos() (x, y float64) {
	i.mu.Lock()
	defer i.mu.Unlock()
	return i.mouseX, i.mouseY
}

func (i *Input) GetScroll() (x, y float64) {
	i.mu.Lock()
	defer i.mu.Unlock()
	return i.scrollX, i.scrollY
}

func (i *Input) GetButton(button input.MouseButton) input.MouseButtonAction {
	i.mu.Lock()
	defer i.mu.Unlock()
	if int(button) < 3 {
		return i.buttons[button]
	}
	return input.ButtonRelease
}

func (i *Input) GetKey(key input.Key) input.KeyAction {
	i.mu.Lock()
	defer i.mu.Unlock()
	if i.keys[pixKeyToCode(key)] {
		return input.KeyPress
	}
	return input.KeyRelease
}

// pixKeyToCode maps a pix input.Key to a DOM event.code string.
func pixKeyToCode(k input.Key) string {
	if k >= input.KeyA && k <= input.KeyZ {
		return "Key" + string(rune('A'+int(k)-int(input.KeyA)))
	}
	if k >= input.Key0 && k <= input.Key9 {
		return "Digit" + string(rune('0'+int(k)-int(input.Key0)))
	}
	if k >= input.KeyF1 && k <= input.KeyF25 {
		return fmt.Sprintf("F%d", int(k)-int(input.KeyF1)+1)
	}
	switch k {
	case input.KeySpace:
		return "Space"
	case input.KeyEscape:
		return "Escape"
	case input.KeyEnter:
		return "Enter"
	case input.KeyTab:
		return "Tab"
	case input.KeyBackspace:
		return "Backspace"
	case input.KeyInsert:
		return "Insert"
	case input.KeyDelete:
		return "Delete"
	case input.KeyRight:
		return "ArrowRight"
	case input.KeyLeft:
		return "ArrowLeft"
	case input.KeyDown:
		return "ArrowDown"
	case input.KeyUp:
		return "ArrowUp"
	case input.KeyPageUp:
		return "PageUp"
	case input.KeyPageDown:
		return "PageDown"
	case input.KeyHome:
		return "Home"
	case input.KeyEnd:
		return "End"
	case input.KeyLeftShift:
		return "ShiftLeft"
	case input.KeyRightShift:
		return "ShiftRight"
	case input.KeyLeftControl:
		return "ControlLeft"
	case input.KeyRightControl:
		return "ControlRight"
	case input.KeyLeftAlt:
		return "AltLeft"
	case input.KeyRightAlt:
		return "AltRight"
	case input.KeyLeftSuper:
		return "MetaLeft"
	case input.KeyRightSuper:
		return "MetaRight"
	case input.KeyMinus:
		return "Minus"
	case input.KeyEqual:
		return "Equal"
	case input.KeyLeftBracket:
		return "BracketLeft"
	case input.KeyRightBracket:
		return "BracketRight"
	case input.KeyBacklash:
		return "Backslash"
	case input.KeySemicolon:
		return "Semicolon"
	case input.KeyApostrophe:
		return "Quote"
	case input.KeyGraveAccent:
		return "Backquote"
	case input.KeyComma:
		return "Comma"
	case input.KeyPeriod:
		return "Period"
	case input.KeySlash:
		return "Slash"
	}
	return ""
}
