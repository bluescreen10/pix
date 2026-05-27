//go:build js

package util

import (
	"syscall/js"

	"github.com/bluescreen10/dawn-go/wgpu"
)

// Window wraps a canvas element on JS/WASM.
type Window struct {
	canvas js.Value
	w, h   int
	input  *Input
}

// NewWindow looks up a canvas element with id "canvas" or creates one and appends
// it to document.body. The width/height are used as fallback if the canvas has no
// intrinsic size. title sets document.title.
func NewWindow(width, height int, title string) (*Window, error) {
	js.Global().Get("document").Set("title", title)

	canvas := js.Global().Get("document").Call("getElementById", "canvas")
	if canvas.IsNull() || canvas.IsUndefined() {
		canvas = js.Global().Get("document").Call("createElement", "canvas")
		canvas.Set("id", "canvas")
		canvas.Set("width", width)
		canvas.Set("height", height)
		js.Global().Get("document").Get("body").Call("appendChild", canvas)
	}

	w := canvas.Get("width").Int()
	h := canvas.Get("height").Int()
	if w == 0 {
		w = width
	}
	if h == 0 {
		h = height
	}

	return &Window{canvas: canvas, w: w, h: h, input: newInput(canvas)}, nil
}

// SurfaceDescriptor returns a wgpu descriptor pointing at the canvas element.
func (win *Window) SurfaceDescriptor() wgpu.SurfaceDescriptor {
	id := win.canvas.Get("id").String()
	return wgpu.SurfaceDescriptor{CanvasID: id}
}

// Size returns the canvas dimensions in pixels.
func (win *Window) Size() (width, height int) { return win.w, win.h }

// Input returns the input handler for this window.
func (win *Window) Input() *Input { return win.input }

// Run calls fn each frame via requestAnimationFrame until fn returns false.
// It blocks until the loop ends; use this as the last call in main.
func (win *Window) Run(fn func() bool) {
	done := make(chan struct{})
	var raf js.Func
	raf = js.FuncOf(func(this js.Value, args []js.Value) any {
		if fn() {
			js.Global().Call("requestAnimationFrame", raf)
		} else {
			raf.Release()
			close(done)
		}
		return nil
	})
	js.Global().Call("requestAnimationFrame", raf)
	<-done
}
