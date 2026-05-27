//go:build !js

package util

import (
	"github.com/bluescreen10/pix/input/glfwinput"
	"github.com/go-gl/glfw/v3.3/glfw"
)

// Input wraps glfwinput on desktop platforms.
type Input struct{ *glfwinput.Input }

func newInput(w *glfw.Window) *Input {
	return &Input{glfwinput.New(w)}
}
