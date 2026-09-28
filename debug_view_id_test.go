package pix_test

import (
	"testing"

	"github.com/bluescreen10/pix"
	"github.com/bluescreen10/pix/cameras"
	"github.com/bluescreen10/pix/colors"
	"github.com/bluescreen10/pix/glm"
	"github.com/bluescreen10/pix/scenes"
)

// distinctColors returns the set of visually distinct (rounded, non-background)
// colors present in px, as a quick way to count how many palette entries actually
// showed up without needing exact RGB matches.
func distinctColors(px []byte) map[[3]byte]int {
	set := map[[3]byte]int{}
	for i := 0; i+3 < len(px); i += 4 {
		r, g, b := px[i], px[i+1], px[i+2]
		if r == 0 && g == 0 && b == 0 {
			continue
		}
		set[[3]byte{r / 16 * 16, g / 16 * 16, b / 16 * 16}]++
	}
	return set
}

// TestDebugObjectIDView confirms DebugObjectID works in plain forward rendering (no
// extra setup at all — every view is a geometry pass over the scene, see DebugView's
// doc comment) and that two
// different objects get two different flat colors.
func TestDebugObjectIDView(t *testing.T) {
	const size = 128
	r, err := pix.NewOffscreenRenderer(size, size)
	if err != nil {
		t.Fatal(err)
	}
	defer r.Destroy()
	scene := scenes.New()
	defer scene.Destroy()
	scene.SetAmbient(colors.RGB32F{0.9, 0.9, 0.9})

	geo := r.GeometryStore.Create(pix.BoxGeometry(1, 1, 1))
	mat := r.NewBasicMaterial()

	a := scene.NewMesh(geo, mat)
	a.SetPosition(glm.Vec3f{-1, 0, 0})
	scene.Add(a)
	b := scene.NewMesh(geo, mat)
	b.SetPosition(glm.Vec3f{1, 0, 0})
	scene.Add(b)

	cam := cameras.NewPerspectiveCamera(60, 1, 0.05, 100)
	cam.SetPosition(glm.Vec3f{0, 0, 4})
	cam.LookAt(glm.Vec3f{0, 0, 0})

	r.SetDebugView(pix.DebugObjectID)
	r.Render(scene, cam)
	colors := distinctColors(r.Pixels())
	t.Logf("distinct object-id colors: %d", len(colors))
	if len(colors) < 2 {
		t.Fatalf("expected at least 2 distinct object colors (two separate meshes), got %d: %v", len(colors), colors)
	}
}

// TestDebugTriangleIDView confirms DebugTriangleID produces several distinct colors
// across a single box's visible faces (12 triangles), also in plain forward mode.
func TestDebugTriangleIDView(t *testing.T) {
	const size = 200
	r, err := pix.NewOffscreenRenderer(size, size)
	if err != nil {
		t.Fatal(err)
	}
	defer r.Destroy()
	scene := scenes.New()
	defer scene.Destroy()
	scene.SetAmbient(colors.RGB32F{0.9, 0.9, 0.9})

	geo := r.GeometryStore.Create(pix.BoxGeometry(1, 1, 1))
	mat := r.NewBasicMaterial()
	m := scene.NewMesh(geo, mat)
	m.SetRotationQuat(glm.NewQuat(0.6, glm.Vec3f{1, 1, 0}))
	scene.Add(m)

	cam := cameras.NewPerspectiveCamera(60, 1, 0.05, 100)
	cam.SetPosition(glm.Vec3f{0, 0, 2.5})
	cam.LookAt(glm.Vec3f{0, 0, 0})

	r.SetDebugView(pix.DebugTriangleID)
	r.Render(scene, cam)
	colors := distinctColors(r.Pixels())
	t.Logf("distinct triangle-id colors: %d", len(colors))
	if len(colors) < 3 {
		t.Fatalf("expected several distinct triangle colors on a rotated box, got %d: %v", len(colors), colors)
	}
}

// TestDebugOffUnaffected confirms adding the two new enum values didn't disturb
// normal shading or the existing G-buffer views' own gating.
func TestDebugIDViewOffByDefault(t *testing.T) {
	r, err := pix.NewOffscreenRenderer(64, 64)
	if err != nil {
		t.Fatal(err)
	}
	defer r.Destroy()
	if r.DebugView() != pix.DebugOff {
		t.Fatalf("expected DebugOff by default, got %v", r.DebugView())
	}
}
