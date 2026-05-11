package main

import (
	"fmt"
	"runtime"

	"github.com/bluescreen10/dawn-go/wgpuglfw"
	"github.com/bluescreen10/pix"
	"github.com/bluescreen10/pix/cameras"
	"github.com/bluescreen10/pix/controls"
	"github.com/bluescreen10/pix/glm"
	"github.com/bluescreen10/pix/input/glfwinput"
	"github.com/bluescreen10/pix/loaders"
	"github.com/go-gl/glfw/v3.3/glfw"

	_ "image/png"
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
	// initialize glfw
	if err := glfw.Init(); err != nil {
		panic(err)
	}

	glfw.WindowHint(glfw.ClientAPI, glfw.NoAPI)

	// create window
	window, err := glfw.CreateWindow(width, height, title, nil, nil)
	if err != nil {
		panic(err)
	}

	// get framebuffer size and surface descriptor
	width, height := window.GetFramebufferSize()
	descriptor := wgpuglfw.GetSurfaceDescriptor(window)

	// create & initialize renderer
	renderer := pix.NewRenderer(uint32(width), uint32(height))
	if err := renderer.Init(descriptor); err != nil {
		panic(err)
	}

	// create input handler
	input := glfwinput.New(window)

	// create camera
	camera := cameras.NewPerpectiveCamera(45, float32(width)/float32(height), 0.01, 2000)
	camera.SetPosition(glm.Vec3f{1, 1, -2})

	// create camera control
	ctrl := controls.NewOrbit(camera, input)
	ctrl.SetPitch(glm.ToRadians(float32(-45)))
	ctrl.SetYaw(glm.ToRadians(float32(45)))

	tex, err := loaders.LoadTexture("assets/uv_grid.png")
	if err != nil {
		panic(err)
	}

	// create material
	material := pix.NewBasicMaterial()
	material.SetColorMap(tex)

	// create geometry
	geo := pix.NewBoxGeometry(1, 1, 1)

	// create mesh
	mesh := pix.NewMesh(geo, material.Build())

	// add mesh to scene
	scene := pix.NewScene()
	scene.Add(mesh)

	var count int
	// main render loop
	for !window.ShouldClose() {

		// render scene
		renderer.Render(scene, camera)

		// update camera control
		ctrl.Update()

		count++
		if count%60 == 0 {
			fmt.Printf("FPS:%.02f\n", renderer.Stats.FPS())
			fmt.Printf("GPUTime:%s\n", renderer.Stats.AvgGPUTime())
			fmt.Printf("CPUTime:%s\n", renderer.Stats.AvgFrameTime())
		}

		// poll events
		glfw.PollEvents()
	}
}
