package pix_test

import (
	"testing"

	"github.com/bluescreen10/pix"
	"github.com/bluescreen10/pix/colors"
	"github.com/bluescreen10/pix/glm"
	"github.com/bluescreen10/pix/scenes"
)

// TestAmbientIntensityScalesTheAmbientLight: lit by ambient light alone, a surface is as
// bright under a colour at intensity 2 as under twice that colour at intensity 1, and
// dimmer at intensity 1, and dark at intensity 0.
func TestAmbientIntensityScalesTheAmbientLight(t *testing.T) {
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

	center := func() byte {
		r.Render(scene)
		return r.Pixels()[(16*32+16)*4]
	}

	scene.SetAmbient(colors.RGB32F{0.5, 0.5, 0.5}, 1)
	twiceTheColor := center()
	scene.SetAmbient(colors.RGB32F{0.25, 0.25, 0.25}, 2)
	twiceTheIntensity := center()
	if d := int(twiceTheIntensity) - int(twiceTheColor); d < -1 || d > 1 {
		t.Errorf("surface = %d under 0.25 at intensity 2, %d under 0.5 at intensity 1; want them equal", twiceTheIntensity, twiceTheColor)
	}
	scene.SetAmbient(colors.RGB32F{0.25, 0.25, 0.25}, 1)
	if got := center(); got >= twiceTheIntensity {
		t.Errorf("surface = %d under 0.25 at intensity 1, %d at 2; want it darker", got, twiceTheIntensity)
	}
	scene.SetAmbient(colors.RGB32F{0.25, 0.25, 0.25}, 0)
	if got := center(); got != 0 {
		t.Errorf("surface = %d at ambient intensity 0, want black", got)
	}
}
