package render_test

import (
	"math"
	"testing"

	"github.com/bluescreen10/pix/colors"
	"github.com/bluescreen10/pix/glm"
	"github.com/bluescreen10/pix/render"
	"github.com/bluescreen10/pix/render/materials"
	"github.com/bluescreen10/pix/render/scenes"
)

// renderHeadOn renders a ground at y = 0 in a Blinn-Phong material set up by configure,
// with no ambient light, under a sun of intensity 1 shining straight down, seen straight
// down by an orthographic camera, and returns the middle pixel: there the normal, the
// light and the eye all line up, so N·L, N·H and V·H are all 1.
func renderHeadOn(t *testing.T, configure func(*materials.BlinnPhongMaterial)) [3]byte {
	t.Helper()
	r, err := render.NewOffscreenRenderer(postSize, postSize)
	if err != nil {
		t.Fatal(err)
	}
	defer r.Destroy()
	scene := scenes.New()
	defer scene.Destroy()
	scene.SetAmbient(colors.RGB32F{}, 0)
	scene.AddDirectionalLight(glm.Vec3f{0, -1, 0}, colors.RGB32F{1, 1, 1}, 1)

	material := r.NewBlinnPhongMaterial()
	defer material.Release()
	configure(material)
	scene.NewMesh(r.NewPlaneGeometry(40, 40, 1, 1), material)
	cam := scene.NewOrthographicCamera(-4, 4, -4, 4, 0.1, 100)
	cam.SetPosition(glm.Vec3f{0, 10, 0.001})
	cam.LookAt(glm.Vec3f{})

	r.Render(scene)
	return pixelAt(r, postSize/2, postSize/2)
}

// TestBlinnPhongDiffuseIsLambert: a grey surface with no highlight, lit head-on, reflects
// its albedo over π of the light — as PBRMaterial's diffuse does, so one light lights
// both material classes alike.
func TestBlinnPhongDiffuseIsLambert(t *testing.T) {
	got := renderHeadOn(t, func(m *materials.BlinnPhongMaterial) {
		m.SetColor(colors.RGBA32F{0.5, 0.5, 0.5, 1})
		m.SetSpecular(colors.RGB32F{})
	})

	want := srgbByte(0.5 / math.Pi)
	if !isWithin(got[0], byte(want), 2) {
		t.Errorf("head-on diffuse = %d, want %d (albedo 0.5 / π)", got[0], want)
	}
}

// TestBlinnPhongHighlightIsNormalized: seen head-on, a black surface shows only its
// highlight, whose peak is specular × (shininess/2 + 1) / 4π. A shinier surface's
// highlight is smaller and brighter, keeping its energy, not merely smaller.
func TestBlinnPhongHighlightIsNormalized(t *testing.T) {
	for _, shininess := range []float32{8, 64} {
		got := renderHeadOn(t, func(m *materials.BlinnPhongMaterial) {
			m.SetColor(colors.RGBA32F{0, 0, 0, 1})
			m.SetSpecular(colors.RGB32F{0.04, 0.08, 0.16})
			m.SetShininess(shininess)
		})

		peak := float32(0.25 * (float64(shininess)/2 + 1) / math.Pi)
		want := [3]int{srgbByte(0.04 * peak), srgbByte(0.08 * peak), srgbByte(0.16 * peak)}
		for c := range 3 {
			if !isWithin(got[c], byte(want[c]), 2) {
				t.Errorf("shininess %v: highlight channel %d = %d, want %d", shininess, c, got[c], want[c])
			}
		}
	}
}
