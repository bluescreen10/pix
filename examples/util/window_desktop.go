//go:build !js

package util

import (
	"runtime"

	"github.com/bluescreen10/dawn-go/wgpu"
	"github.com/bluescreen10/dawn-go/wgpuglfw"
	"github.com/go-gl/glfw/v3.3/glfw"
)

func init() { runtime.LockOSThread() }

// Window wraps a GLFW window on desktop platforms.
type Window struct {
	w     *glfw.Window
	input *Input
}

// NewWindow creates a GLFW window with the given dimensions and title.
func NewWindow(width, height int, title string) (*Window, error) {
	if err := glfw.Init(); err != nil {
		return nil, err
	}
	glfw.WindowHint(glfw.ClientAPI, glfw.NoAPI)
	w, err := glfw.CreateWindow(width, height, title, nil, nil)
	if err != nil {
		return nil, err
	}
	return &Window{w: w, input: newInput(w)}, nil
}

// SurfaceDescriptor returns the wgpu surface descriptor for this window.
func (w *Window) SurfaceDescriptor() wgpu.SurfaceDescriptor {
	return wgpuglfw.GetSurfaceDescriptor(w.w)
}

// Size returns the framebuffer dimensions in pixels.
func (w *Window) Size() (width, height int) {
	return w.w.GetFramebufferSize()
}

// Input returns the input handler for this window.
func (w *Window) Input() *Input { return w.input }

// Run calls fn each frame until fn returns false or the window is closed.
func (w *Window) Run(fn func() bool) {
	for !w.w.ShouldClose() {
		if !fn() {
			break
		}
		glfw.PollEvents()
	}
}
