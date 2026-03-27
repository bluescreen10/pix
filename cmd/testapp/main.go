package main

import (
	"runtime"

	"github.com/bluescreen10/pix"
	"github.com/bluescreen10/pix/cameras"
	"github.com/bluescreen10/pix/glm"
	"github.com/cogentcore/webgpu/wgpuglfw"
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

	camera := cameras.NewPerpectiveCamera(45, float32(width)/float32(height), 0.01, 2000)
	camera.SetPosition(0, 1, -3)

	// tex, err := loaders.LoadTexture("cmd/testapp/assets/uv_grid.png")
	// //ex, err := loaders.LoadTexture("assets/uv_grid.png")
	// if err != nil {
	// 	panic(err)
	// }

	scene := pix.NewScene()

	material := pix.NewBasicMaterial()
	material.SetColor(glm.Color3f{1, 0, 0})
	//material.SetColorMap(tex)

	geo := pix.NewBoxGeometry(1, 1, 1)

	for i := range 100 {
		for j := range 100 {
			mesh := pix.NewMesh(geo, material.Build())
			mesh.SetPosition(float32(i)*4, 0, float32(j)*4)
			//mesh.SetRotation(float32(i), float32(j), 0)
			scene.Add(mesh)
		}
	}

	scene.SetBackground(glm.Color4f{0.5, 0.5, 0, 1})

	for !window.ShouldClose() {
		err := renderer.Render(scene, camera)
		if err != nil {
			panic(err)
		}
		camera.Move(0, 0, -0.1)
		//mesh.Rotate(0, 0.005, 0)
		//mesh.Move(0, 0, 0.05)

		glfw.PollEvents()
	}
}
