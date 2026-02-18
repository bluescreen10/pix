package main

import (
	"runtime"

	"github.com/bluescreen10/pix"
	"github.com/bluescreen10/pix/cameras"
	"github.com/cogentcore/webgpu/wgpuglfw"
	"github.com/go-gl/glfw/v3.3/glfw"
)

func init() {
	runtime.LockOSThread()
}

const (
	width  = 1000
	height = 500
	title  = "Test Application"
)

func main() {
	if err := glfw.Init(); err != nil {
		panic(err)
	}

	glfw.WindowHint(glfw.ClientAPI, glfw.NoAPI)

	window, err := glfw.CreateWindow(width, height, title, nil, nil)
	if err != nil {
		panic(err)
	}

	width, height := window.GetFramebufferSize()
	descriptor := wgpuglfw.GetSurfaceDescriptor(window)

	renderer := pix.NewRenderer(uint32(width), uint32(height))
	if err := renderer.Init(descriptor); err != nil {
		panic(err)
	}

	camera := cameras.NewPerpectiveCamera(45, float32(width)/float32(height), 0.01, 2000)
	camera.SetPosition(0, 0, -5)
	mesh := pix.NewMesh(
		pix.NewBoxGeometry(1, 1, 1),
	)

	for !window.ShouldClose() {
		err := renderer.Render(mesh, camera)
		if err != nil {
			panic(err)
		}
		glfw.PollEvents()
	}
}
