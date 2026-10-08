package pix_test

import (
	"testing"

	"github.com/bluescreen10/pix"
	"github.com/bluescreen10/pix/colors"
	"github.com/bluescreen10/pix/geometries"
	"github.com/bluescreen10/pix/glm"
	"github.com/bluescreen10/pix/materials"
	"github.com/bluescreen10/pix/scenes"
)

// renderGlassOverRed draws glass, lit head-on, over an unlit red backdrop and returns
// the centre pixel. HDR is on, so the glass draws what is behind it from the scene copy,
// as it does in a lit scene, rather than blending over it.
func renderGlassOverRed(t *testing.T, glass func(r *pix.Renderer) *materials.PBRMaterial) [3]uint8 {
	t.Helper()
	const size = 64
	r, err := pix.NewOffscreenRenderer(size, size)
	if err != nil {
		t.Fatal(err)
	}
	defer r.Destroy()
	r.EnableHDR(true)
	r.SetClearColor([4]float32{0, 0, 0, 1})

	quad := func(z float32) geometries.Geometry {
		return r.GeometryStore.Create(geometries.GeometryConfig{
			Attributes: []geometries.Attribute{
				geometries.NewAttribute(geometries.AttributePosition, geometries.Float32x3, []glm.Vec3f{{-0.8, -0.8, z}, {0.8, -0.8, z}, {0.8, 0.8, z}, {-0.8, 0.8, z}}),
				geometries.NewAttribute(geometries.AttributeNormal, geometries.Float32x3, []glm.Vec3f{{0, 0, 1}, {0, 0, 1}, {0, 0, 1}, {0, 0, 1}}),
			},
			Indices: []uint32{0, 1, 2, 0, 2, 3},
		})
	}
	scene := scenes.New()
	defer scene.Destroy()
	scene.AddDirectionalLight(glm.Vec3f{0, 0, -1}, colors.RGB32F{1, 1, 1}, 3)
	pane := glass(r)
	defer pane.Release()
	scene.Add(scene.NewMesh(quad(0), pane))
	red := r.NewBasicMaterial()
	defer red.Release()
	red.SetColor(colors.RGBA32F{1, 0, 0, 1})
	scene.Add(scene.NewMesh(quad(-0.5), red))
	cam := scene.NewPerspectiveCamera(45, 1, 0.1, 1000)
	scene.Add(cam)
	cam.SetPosition(glm.Vec3f{0, 0, 2})

	r.Render(scene)
	pixels := r.Pixels()
	i := (size/2*size + size/2) * 4
	return [3]uint8{pixels[i], pixels[i+1], pixels[i+2]}
}

// newClearGlass is smooth, white glass, alpha its colour's alpha.
func newClearGlass(r *pix.Renderer, alpha float32) *materials.PBRMaterial {
	glass := r.NewPBRMaterial()
	glass.SetColor(colors.RGBA32F{1, 1, 1, alpha})
	glass.SetMetallic(0)
	glass.SetRoughness(0.4)
	glass.SetTransmission(1)
	glass.SetThickness(0.1)
	return glass
}

// TestGlassIgnoringAlphaIsWhole: glass whose alpha is ignored — glTF's alphaMode
// OPAQUE — is whole glass however low its colour's alpha, and draws as glass of alpha 1
// does: its highlight, and the backdrop through it. Bistro's clear shop windows are such
// glass, with a colour map whose alpha is 0 everywhere; taking that alpha as how much of
// the pane is there left no pane at all.
func TestGlassIgnoringAlphaIsWhole(t *testing.T) {
	want := renderGlassOverRed(t, func(r *pix.Renderer) *materials.PBRMaterial {
		return newClearGlass(r, 1)
	})
	got := renderGlassOverRed(t, func(r *pix.Renderer) *materials.PBRMaterial {
		glass := newClearGlass(r, 0)
		glass.SetIgnoresAlpha(true)
		return glass
	})
	for c := range got {
		if diff := int(got[c]) - int(want[c]); diff < -2 || diff > 2 {
			t.Fatalf("glass of alpha 0 ignoring it = %v, want %v as glass of alpha 1", got, want)
		}
	}

	// Without ignoring it, alpha 0 is no glass: the comparison above means something.
	vanished := renderGlassOverRed(t, func(r *pix.Renderer) *materials.PBRMaterial {
		return newClearGlass(r, 0)
	})
	if vanished == want {
		t.Errorf("glass of alpha 0 = %v, the same as glass of alpha 1; want it gone", vanished)
	}
}
