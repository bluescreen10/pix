package pix_test

import (
	"testing"

	"github.com/bluescreen10/pix"
	"github.com/bluescreen10/pix/colors"
	"github.com/bluescreen10/pix/geometries"
	"github.com/bluescreen10/pix/glm"
	"github.com/bluescreen10/pix/scenes"
)

// TestFogBlendsTowardFogColorWithDistance renders the same red quad twice, once with
// no fog and once with a close exp2 fog set to a distinct colour, and checks the
// second render's pixel shifted away from the material's own colour and toward the
// fog colour. This exercises fog end to end — packing it into the GPU light table and
// the shader actually blending with it — through nothing but the public renderer API.
func TestFogBlendsTowardFogColorWithDistance(t *testing.T) {
	const size = 32
	const quadZ = -50 // far from the camera, so a close fog fully engages

	render := func(fog scenes.Fog) (r, g, b byte) {
		ren, err := pix.NewOffscreenRenderer(size, size)
		if err != nil {
			t.Fatal(err)
		}
		defer ren.Destroy()

		scene := scenes.New()
		defer scene.Destroy()
		scene.SetAmbient(colors.RGB32F{1, 1, 1}) // full ambient -> albedo shows directly
		scene.SetFog(fog)

		geo := ren.GeometryStore.Create(geometries.GeometryConfig{
			Attributes: []geometries.Attribute{
				geometries.NewAttribute(geometries.AttributePosition, geometries.Float32x3, []glm.Vec3f{
					{-40, -40, quadZ}, {40, -40, quadZ}, {40, 40, quadZ}, {-40, 40, quadZ},
				}),
			},
			Indices: []uint32{0, 1, 2, 0, 2, 3},
		})
		mat := ren.NewBasicMaterial()
		mat.SetColor(colors.RGBA32F{1, 0, 0, 1})
		scene.Add(scene.NewMesh(geo, mat))

		cam := scene.NewPerspectiveCamera(45, 1, 0.1, 1000)
		scene.Add(cam)
		cam.SetPosition(glm.Vec3f{0, 0, 10}) // looks down -Z at the origin by default, past the quad
		ren.Render(scene)

		px := ren.Pixels()
		i := (size/2*size + size/2) * 4
		return px[i], px[i+1], px[i+2]
	}

	baseR, baseG, baseB := render(nil)
	t.Logf("no fog: (%d,%d,%d)", baseR, baseG, baseB)
	if baseR < 200 || baseG > 40 || baseB > 40 {
		t.Fatalf("unfogged quad = (%d,%d,%d), want close to pure red", baseR, baseG, baseB)
	}

	fog := scenes.NewExp2Fog(colors.RGB32F{0, 0, 1}, 10) // closes in well before the quad
	fogR, fogG, fogB := render(fog)
	t.Logf("blue fog: (%d,%d,%d)", fogR, fogG, fogB)
	if fogB <= baseB {
		t.Fatalf("fogged pixel blue channel = %d, want it higher than the unfogged %d (should shift toward the blue fog colour)", fogB, baseB)
	}
	if fogR >= baseR {
		t.Fatalf("fogged pixel red channel = %d, want it lower than the unfogged %d (the material's own colour should fade)", fogR, baseR)
	}
}
