package main

import (
	"bytes"
	_ "embed"
	"fmt"
	"image"
	_ "image/png"

	"github.com/bluescreen10/dawn-go/wgpu"
	"github.com/bluescreen10/pix"
	"github.com/bluescreen10/pix/cameras"
	"github.com/bluescreen10/pix/controls"
	"github.com/bluescreen10/pix/examples/util"
	"github.com/bluescreen10/pix/glm"
)

//go:embed assets/uv_grid.png
var uvGridPNG []byte

const (
	winWidth  = 1000
	winHeight = 500
	title     = "Hello Cube"
)

func main() {
	win, err := util.NewWindow(winWidth, winHeight, title)
	if err != nil {
		panic(err)
	}

	w, h := win.Size()
	renderer := pix.NewRenderer(uint32(w), uint32(h))
	if err := renderer.Init(win.SurfaceDescriptor()); err != nil {
		panic(err)
	}

	img, _, err := image.Decode(bytes.NewReader(uvGridPNG))
	if err != nil {
		panic(err)
	}
	rgba := image.NewRGBA(img.Bounds())
	for y := range rgba.Bounds().Dy() {
		for x := range rgba.Bounds().Dx() {
			rgba.Set(x, y, img.At(x, y))
		}
	}
	td := pix.NewDataTexture(rgba.Pix, rgba.Bounds().Dx(), rgba.Bounds().Dy(), wgpu.TextureFormatRGBA8Unorm)
	tex := renderer.NewTexture(td)

	material := renderer.NewBasicMaterial()
	material.SetColorMap(tex)
	tex.Release()

	geo := renderer.NewBoxGeometry(1, 1, 1)
	scene := pix.NewScene()
	mesh := scene.NewMesh(geo, material.Ref())
	geo.Release()
	material.Release()
	scene.Add(mesh)

	camera := cameras.NewPerpectiveCamera(45, float32(w)/float32(h), 0.01, 2000)
	camera.SetPosition(glm.Vec3f{1, 1, -2})

	ctrl := controls.NewOrbit(camera, win.Input())
	ctrl.SetPitch(glm.ToRadians(float32(-45)))
	ctrl.SetYaw(glm.ToRadians(float32(45)))

	var count int
	win.Run(func() bool {
		renderer.Render(scene, camera)
		ctrl.Update()
		count++
		if count%60 == 0 {
			fmt.Printf("FPS:%.02f\n", renderer.Stats.FPS())
			fmt.Printf("GPUTime:%s\n", renderer.Stats.AvgGPUTime())
			fmt.Printf("CPUTime:%s\n", renderer.Stats.AvgFrameTime())
		}
		return true
	})
}
