package pix

import (
	"testing"

	"github.com/bluescreen10/pix/cameras"
	"github.com/bluescreen10/pix/colors"
	"github.com/bluescreen10/pix/geometries"
	"github.com/bluescreen10/pix/glm"
)

// TestTransparency renders an opaque red quad behind a 50%-alpha blue quad in front.
// The overlap must blend (both channels present) — proving alpha blending, the
// opaque-before-transparent ordering, and depth-test-but-no-depth-write.
func TestTransparency(t *testing.T) {
	const size = 96
	r, err := NewOffscreenRenderer(size, size)
	if err != nil {
		t.Fatal(err)
	}
	defer r.Destroy()
	r.SetClearColor([4]float32{0, 0, 0, 1})
	scene := r.NewScene()
	defer scene.Destroy()
	scene.SetAmbient(colors.RGB32F{1, 1, 1}) // full ambient → albedo shows directly

	quad := func(z float32) geometries.Geometry {
		return r.GeometryStore.Create(geometries.GeometryConfig{
			Attributes: []geometries.Attribute{
				geometries.NewAttribute(geometries.AttributePosition, geometries.Float32x3, []glm.Vec3f{{-0.8, -0.8, z}, {0.8, -0.8, z}, {0.8, 0.8, z}, {-0.8, 0.8, z}}),
			},
			Indices: []uint32{0, 1, 2, 0, 2, 3},
		})
	}

	// Opaque red behind. Add it LAST to prove the sort still draws opaque first.
	red := r.NewPBRMaterial()
	red.SetColor(colors.RGBA32F{1, 0, 0, 1})
	// Transparent blue in front (added first).
	blue := r.NewPBRMaterial()
	blue.SetColor(colors.RGBA32F{0, 0, 1, 0.5})
	blue.SetBlend(BlendAlpha)
	scene.Add(scene.NewMesh(quad(0), blue))   // front, transparent, added first
	scene.Add(scene.NewMesh(quad(-0.5), red)) // behind, opaque, added last

	cam := cameras.NewPerspectiveCamera(45, 1, 0.1, 1000)
	cam.SetPosition(glm.Vec3f{0, 0, 2})
	r.Render(scene, cam)

	px := r.Pixels()
	i := (size/2*size + size/2) * 4 // center (overlap)
	rr, gg, bb := px[i], px[i+1], px[i+2]
	t.Logf("center pixel = (%d,%d,%d)", rr, gg, bb)
	if rr < 40 || bb < 40 {
		t.Fatalf("no blend at overlap: (%d,%d,%d) — red should show through the blue", rr, gg, bb)
	}
}
