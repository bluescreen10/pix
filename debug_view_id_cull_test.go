package pix_test

import (
	"testing"

	"github.com/bluescreen10/pix"
	"github.com/bluescreen10/pix/colors"
	"github.com/bluescreen10/pix/glm"
	"github.com/bluescreen10/pix/materials"
	"github.com/bluescreen10/pix/scenes"
)

// TestDebugIDShowsDoubleSidedBackfaces confirms recordDebugIDView doesn't drop a
// double-sided (CullNone) material's backfaces — it used to hardcode CullBack for
// every drawable regardless of that drawable's own material, which silently dropped
// geometry (foliage, glass, anything double-sided) that a normal render shows fine.
// The camera sits INSIDE a CullNone box, looking at an interior wall: from there,
// every visible triangle is a backface relative to the box's own (outward-facing)
// winding, so a fixed CullBack debug pass would show nothing at all here.
func TestDebugIDShowsDoubleSidedBackfaces(t *testing.T) {
	const size = 64
	r, err := pix.NewOffscreenRenderer(size, size)
	if err != nil {
		t.Fatal(err)
	}
	defer r.Destroy()
	scene := scenes.New()
	defer scene.Destroy()
	scene.SetAmbient(colors.RGB32F{0.9, 0.9, 0.9})

	geo := r.GeometryStore.Create(pix.BoxGeometry(10, 10, 10))
	mat := r.NewBasicMaterial()
	mat.SetCull(materials.CullNone)
	box := scene.NewMesh(geo, mat)
	scene.Add(box)

	cam := scene.NewPerspectiveCamera(90, 1, 0.05, 100)
	scene.Add(cam)
	cam.SetPosition(glm.Vec3f{0, 0, 0}) // inside the box
	cam.LookAt(glm.Vec3f{0, 0, -1})

	r.SetDebugView(pix.DebugObjectID)
	r.Render(scene)
	px := r.Pixels()
	lit := 0
	for i := 0; i+3 < len(px); i += 4 {
		if px[i] != 0 || px[i+1] != 0 || px[i+2] != 0 {
			lit++
		}
	}
	t.Logf("lit pixels: %d / %d", lit, size*size)
	if lit < size*size/2 {
		t.Fatalf("expected the interior wall to fill most of the frame in DebugObjectID, got %d/%d lit pixels", lit, size*size)
	}
}
