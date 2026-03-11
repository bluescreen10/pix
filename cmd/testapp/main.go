package main

import (
	"runtime"

	"github.com/bluescreen10/pix"
	"github.com/bluescreen10/pix/cameras"
	"github.com/bluescreen10/pix/glm"
	"github.com/bluescreen10/pix/loaders"
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
	camera.SetPosition(1, 0.5, -3)

	tex, err := loaders.LoadTexture("cmd/testapp/assets/uv_grid.png")
	//ex, err := loaders.LoadTexture("assets/uv_grid.png")
	if err != nil {
		panic(err)
	}

	material := pix.NewBasicMaterial()
	material.SetColor(glm.Color3f{0, 1, 1})
	material.SetColorMap(tex)

	mesh := pix.NewMesh(
		pix.NewBoxGeometry(1, 1, 1),
		material.Material,
	)

	scene := &pix.Scene{}
	scene.Add(mesh)
	scene.SetBackground(glm.Color4f{0.5, 0.5, 0, 1})

	var count int
	var flip bool

	for !window.ShouldClose() {
		err := renderer.Render(scene, camera)
		if err != nil {
			panic(err)
		}
		camera.Move(-0.01, 0, 0.00)
		count++
		if count%100 == 0 {
			if flip {
				//material.SetColor(glm.Color3f{1, 0, 0})
				//td.SetAddressModeU(wgpu.AddressModeClampToEdge)
				// td.SetMinFilter(wgpu.FilterModeLinear)
				// td.SetMagFilter(wgpu.FilterModeLinear)
				// td.SetMipmapFilter(wgpu.MipmapFilterModeLinear)

				// td.SetMaxAnisotropy(16)
				// td.SetLodMaxClamp(1)
				// td.SetLodMinClamp(0)
			} else {
				//material.SetColor(glm.Color3f{0, 1, 0})

				// td.SetMinFilter(wgpu.FilterModeNearest)
				// td.SetMagFilter(wgpu.FilterModeNearest)
				// td.SetMipmapFilter(wgpu.MipmapFilterModeNearest)

				// td.SetMaxAnisotropy(1)
				// td.SetLodMaxClamp(10)
				// td.SetLodMinClamp(1)
			}
			flip = !flip
		}
		glfw.PollEvents()
	}
}
