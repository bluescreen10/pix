package postprocess_test

import (
	"testing"

	"github.com/bluescreen10/pix"
	"github.com/bluescreen10/pix/colors"
	"github.com/bluescreen10/pix/geometries"
	"github.com/bluescreen10/pix/glm"
	"github.com/bluescreen10/pix/scenes"
)

const postSize = 96

// postScene renders one unlit quad of the given colour, covering the middle of the frame
// by the given fraction, over a black background. Unlit means the colour reaches the
// scene image as-is, so a colour above 1.0 is light above white.
func postScene(t *testing.T, color colors.RGBA32F, coverage float32) (*pix.Renderer, *scenes.Scene) {
	t.Helper()
	r, err := pix.NewOffscreenRenderer(postSize, postSize)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(r.Destroy)
	r.SetClearColor(colors.RGBA32F{0, 0, 0, 1})

	scene := scenes.New()
	t.Cleanup(scene.Destroy)
	h := coverage
	quad := r.GeometryStore.Create(geometries.GeometryConfig{
		Attributes: []geometries.Attribute{
			geometries.NewAttribute(geometries.AttributePosition, geometries.Float32x3, []glm.Vec3f{{-h, -h, 0}, {h, -h, 0}, {h, h, 0}, {-h, h, 0}}),
		},
		Indices: []uint32{0, 1, 2, 0, 2, 3},
	})
	material := r.NewBasicMaterial()
	material.SetColor(color)
	scene.NewMesh(quad, material)

	// From 2 units away a 45-degree camera sees about ±0.83 of the plane z = 0, so a
	// coverage of 0.8 fills nearly the frame and 0.2 the middle quarter of it.
	cam := scene.NewPerspectiveCamera(45, 1, 0.1, 100)
	cam.SetPosition(glm.Vec3f{0, 0, 2})
	return r, scene
}

func pixelAt(r *pix.Renderer, x, y int) [3]byte {
	px := r.Pixels()
	i := (y*postSize + x) * 4
	return [3]byte{px[i], px[i+1], px[i+2]}
}
