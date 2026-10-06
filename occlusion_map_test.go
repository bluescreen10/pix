package pix_test

import (
	"math"
	"testing"

	"github.com/bluescreen10/pix"
	"github.com/bluescreen10/pix/colors"
	"github.com/bluescreen10/pix/glm"
	"github.com/bluescreen10/pix/scenes"
	"github.com/bluescreen10/pix/textures"
)

// TestOcclusionMapDarkensOnlyIndirectLight: a PBR material's occlusion map scales the
// ambient light that reaches it by the map's red channel, as far as its strength says,
// and leaves the sun's light alone.
func TestOcclusionMapDarkensOnlyIndirectLight(t *testing.T) {
	r, err := pix.NewOffscreenRenderer(32, 32)
	if err != nil {
		t.Fatal(err)
	}
	defer r.Destroy()
	scene := scenes.New()
	defer scene.Destroy()
	material := r.NewPBRMaterial()
	material.SetRoughness(1)
	scene.Add(scene.NewMesh(r.NewBoxGeometry(4, 4, 1), material))
	camera := scene.NewPerspectiveCamera(45, 1, 0.1, 100)
	scene.Add(camera)
	camera.SetPosition(glm.Vec3f{0, 0, 3})
	camera.LookAt(glm.Vec3f{})
	// A quarter of the light around it reaches the surface: red 64 of 255.
	occlusion := r.TextureStore.Create([]byte{64, 255, 255, 255}, 1, 1, textures.Linear)
	defer occlusion.Release()

	center := func() float64 {
		r.Render(scene)
		return linearFromSRGB(r.Pixels()[(16*32+16)*4])
	}

	scene.SetAmbient(colors.RGB32F{0.5, 0.5, 0.5}, 1)
	open := center()
	material.SetOcclusionMap(occlusion)
	if got, want := center(), open*64/255; math.Abs(got-want) > 0.02 {
		t.Errorf("surface under ambient light = %.3f with the occlusion map, %.3f without; want %.3f, the map's share", got, open, want)
	}
	material.SetOcclusionStrength(0)
	if got := center(); math.Abs(got-open) > 0.01 {
		t.Errorf("surface = %.3f with the occlusion map at strength 0, %.3f without; want it unchanged", got, open)
	}

	material.SetOcclusionStrength(1)
	scene.SetAmbient(colors.RGB32F{}, 0)
	scene.AddDirectionalLight(glm.Vec3f{0, 0, -1}, colors.RGB32F{1, 1, 1}, 1)
	occluded := center()
	material.SetOcclusionMap(textures.Texture{})
	if lit := center(); math.Abs(occluded-lit) > 0.01 {
		t.Errorf("surface under the sun alone = %.3f with the occlusion map, %.3f without; want it unchanged", occluded, lit)
	}
}
