package pix_test

import (
	"testing"

	"github.com/bluescreen10/pix"
	"github.com/bluescreen10/pix/colors"
	"github.com/bluescreen10/pix/glm"
	"github.com/bluescreen10/pix/scenes"
)

// TestDepthDebugPolarity confirms near reads dark, far reads brighter, and background
// (nothing drawn there) reads brightest — with a large near/far ratio (~12000:1, matching
// examples/beach's cameras) so the log2 remap actually gets exercised. The old
// sqrt(1-d) remap saturated almost everywhere once near/far exceeded a few hundred:1,
// since reversed-Z d is approximately near/z: any on-screen content beyond a few
// hundred units already had d well under 0.1, and both (1-d) and its sqrt stayed close
// to 1 regardless of how much farther the actual geometry was.
//
// Background is the part a geometry pass does not get for free: the G-buffer view this
// replaces shaded every pixel, so undrawn area landed at the far-most value on its own.
// Here nothing is drawn there, so the pass clears the depth view to white to put it at
// the same end of the ramp (see debugClear).
func TestDepthDebugPolarity(t *testing.T) {
	const size = 64
	r, err := pix.NewOffscreenRenderer(size, size)
	if err != nil {
		t.Fatal(err)
	}
	defer r.Destroy()
	scene := scenes.New()
	defer scene.Destroy()
	scene.SetAmbient(colors.RGB32F{0.9, 0.9, 0.9})

	mat := r.NewPBRMaterial()
	nearGeo := r.GeometryStore.Create(pix.BoxGeometry(10, 10, 10))
	nearBox := scene.NewMesh(nearGeo, mat)
	nearBox.SetPosition(glm.Vec3f{-15, 0, -100})
	scene.Add(nearBox)
	farGeo := r.GeometryStore.Create(pix.BoxGeometry(2000, 2000, 2000))
	farBox := scene.NewMesh(farGeo, mat)
	farBox.SetPosition(glm.Vec3f{15, 0, -20000})
	scene.Add(farBox)

	cam := scene.NewPerspectiveCamera(60, 1, 30, 360000)
	scene.Add(cam)
	cam.SetPosition(glm.Vec3f{0, 0, 0})
	cam.LookAt(glm.Vec3f{0, 0, -1})

	// Find each box's actual screen position from a normal render (rather than
	// guessing projected coordinates by hand), then sample the depth-debug frame at
	// those same pixels, plus a corner known to be neither box.
	r.Render(scene)
	normalPx := append([]byte(nil), r.Pixels()...)
	r.SetDebugView(pix.DebugDepth)
	r.Render(scene)
	depthPx := r.Pixels()

	firstLit := func(fromLeft bool) (int, int) {
		for x := 0; x < size; x++ {
			xx := x
			if !fromLeft {
				xx = size - 1 - x
			}
			for y := 0; y < size; y++ {
				i := (y*size + xx) * 4
				if normalPx[i] != 0 || normalPx[i+1] != 0 || normalPx[i+2] != 0 {
					return xx, y
				}
			}
		}
		t.Fatal("no lit pixel found")
		return 0, 0
	}
	sampleAt := func(x, y int) float64 {
		i := (y*size + x) * 4
		return float64(depthPx[i]) / 255
	}

	nx, ny := firstLit(true)
	fx, fy := firstLit(false)
	near := sampleAt(nx, ny)
	far := sampleAt(fx, fy)
	background := sampleAt(0, 0)
	t.Logf("near=%.3f (at %d,%d) far=%.3f (at %d,%d) background=%.3f", near, nx, ny, far, fx, fy, background)

	if !(near < far && far < background) {
		t.Fatalf("expected near < far < background, got near=%.3f far=%.3f background=%.3f", near, far, background)
	}
	if near > 0.4 {
		t.Errorf("near geometry should read clearly dark, got %.3f", near)
	}
	if background < 0.9 {
		t.Errorf("background (nothing drawn) should read near-white, got %.3f", background)
	}
}
