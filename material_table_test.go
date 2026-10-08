package pix_test

import (
	"testing"

	"github.com/bluescreen10/pix"
	"github.com/bluescreen10/pix/colors"
	"github.com/bluescreen10/pix/glm"
	"github.com/bluescreen10/pix/materials"
	"github.com/bluescreen10/pix/scenes"
)

// The drawables a scene emits reference materials through a distinct set internally
// (rather than one entry each), which is what makes the renderer's per-frame pipeline
// work proportional to the materials in play instead of the objects on screen. These
// tests pin the correctness half of that from the outside: every drawable still renders
// with its own material's actual appearance, however the renderer resolves the
// indirection internally.

// TestMaterialTableDedupsAcrossInstances is the case the indirection exists for: an
// InstancedMesh emits one drawable per instance, and every one of them shares a single
// material. Rendered as a wide row, the whole row must show that material's color —
// nothing about sharing one material across many instances may corrupt any of them.
func TestMaterialTableDedupsAcrossInstances(t *testing.T) {
	r, err := pix.NewOffscreenRenderer(256, 32)
	if err != nil {
		t.Fatal(err)
	}
	defer r.Destroy()
	scene := scenes.New()
	defer scene.Destroy()
	scene.SetAmbient(colors.RGB32F{1, 1, 1}, 1)

	geo := r.GeometryStore.Create(pix.BoxGeometry(1, 1, 1))
	defer geo.Release()
	mat := r.NewBasicMaterial()
	mat.SetColor(colors.RGBA32F{0, 1, 0, 1})

	const instances = 16
	xforms := make([]glm.Mat4f, instances)
	for i := range xforms {
		xforms[i] = glm.Transform(glm.Vec3f{2, 2, 2}, glm.QuatfIdentity, glm.Vec3f{float32(i)*8 - 4*float32(instances), 0, 0})
	}
	scene.NewInstancedMesh(geo, mat, xforms)

	cam := scene.NewPerspectiveCamera(70, 8, 0.1, 200)
	cam.SetPosition(glm.Vec3f{0, 0, 20})
	r.Render(scene)

	const w, h = 256, 32
	px := r.Pixels()
	firstGreen, lastGreen := -1, -1
	for x := range w {
		for y := range h {
			i := (y*w + x) * 4
			r, g, b := px[i], px[i+1], px[i+2]
			if r == 0 && g == 0 && b == 0 {
				continue
			}
			if g <= 100 || r >= 60 || b >= 60 {
				t.Fatalf("pixel (%d,%d) = (%d,%d,%d) does not match the shared material's colour — an instance's material resolved wrong", x, y, r, g, b)
			}
			if firstGreen < 0 {
				firstGreen = x
			}
			lastGreen = x
		}
	}
	t.Logf("green span: %d..%d", firstGreen, lastGreen)
	if firstGreen < 0 {
		t.Fatal("no green pixels — the instanced mesh did not render")
	}
	if span := lastGreen - firstGreen; span < w/2 {
		t.Fatalf("green span = %d px, want > %d — expected instances spread across most of the frame's width", span, w/2)
	}
}

// TestMaterialTableKeepsDistinctMaterialsApart is the other half: dedup must key on
// material identity, so two materials of the same TYPE stay separate — each mesh must
// keep showing its own material's colour, not another mesh's.
func TestMaterialTableKeepsDistinctMaterialsApart(t *testing.T) {
	r, err := pix.NewOffscreenRenderer(240, 80)
	if err != nil {
		t.Fatal(err)
	}
	defer r.Destroy()
	scene := scenes.New()
	defer scene.Destroy()
	scene.SetAmbient(colors.RGB32F{1, 1, 1}, 1)

	geo := r.GeometryStore.Create(pix.BoxGeometry(1, 1, 1))
	defer geo.Release()

	red, blue := r.NewBasicMaterial(), r.NewBasicMaterial()
	red.SetColor(colors.RGBA32F{1, 0, 0, 1})
	blue.SetColor(colors.RGBA32F{0, 0, 1, 1})

	place := func(m materials.Material, x float32) {
		mesh := scene.NewMesh(geo, m)
		mesh.SetPosition(glm.Vec3f{x, 0, 0})
	}
	// Two meshes on red, one on blue, spread left to right with a gap between them.
	place(red, -6)
	place(red, 0)
	place(blue, 6)

	cam := scene.NewPerspectiveCamera(35, 3, 0.1, 100)
	cam.SetPosition(glm.Vec3f{0, 0, 10})
	r.Render(scene)

	const w, h = 240, 80
	px := r.Pixels()
	third := w / 3
	dominant := func(x0, x1 int) (red, blue int) {
		for y := range h {
			for x := x0; x < x1; x++ {
				i := (y*w + x) * 4
				if px[i] > 150 && px[i+2] < 60 {
					red++
				}
				if px[i+2] > 150 && px[i] < 60 {
					blue++
				}
			}
		}
		return
	}
	leftR, leftB := dominant(0, third)
	midR, midB := dominant(third, 2*third)
	rightR, rightB := dominant(2*third, w)
	t.Logf("left red=%d blue=%d | mid red=%d blue=%d | right red=%d blue=%d", leftR, leftB, midR, midB, rightR, rightB)

	if leftR == 0 || leftB != 0 {
		t.Errorf("left third (red mesh) = red %d blue %d, want red only", leftR, leftB)
	}
	if midR == 0 || midB != 0 {
		t.Errorf("middle third (red mesh) = red %d blue %d, want red only", midR, midB)
	}
	if rightB == 0 || rightR != 0 {
		t.Errorf("right third (blue mesh) = red %d blue %d, want blue only", rightR, rightB)
	}
}
