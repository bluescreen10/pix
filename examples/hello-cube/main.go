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

	input := glfwinput.New(window)

	camera := cameras.NewPerpectiveCamera(45, float32(width)/float32(height), 0.01, 2000)
	camera.SetPosition(glm.Vec3f{1, 1, -2})

	ctrl := controls.NewOrbit(camera, input)
	ctrl.SetPitch(glm.ToRadians(float32(-45)))
	ctrl.SetYaw(glm.ToRadians(float32(45)))

	tex, err := loaders.LoadTexture(renderer, "assets/uv_grid.png")
	if err != nil {
		panic(err)
	}

	material := renderer.NewBasicMaterial()
	material.SetColorMap(tex)
	tex.Release()

	geo := renderer.NewBoxGeometry(1, 1, 1)

	scene := pix.NewScene()
	mesh := scene.NewMesh(geo, material.Ref())
	geo.Release()
	material.Release()
	scene.Add(mesh)

	var count int
	for !window.ShouldClose() {
		renderer.Render(scene, camera)

		ctrl.Update()

		count++
		if count%60 == 0 {
			fmt.Printf("FPS:%.02f\n", renderer.Stats.FPS())
			fmt.Printf("GPUTime:%s\n", renderer.Stats.AvgGPUTime())
			fmt.Printf("CPUTime:%s\n", renderer.Stats.AvgFrameTime())
		}

		glfw.PollEvents()
	}
}
