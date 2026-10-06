package pix_test

import (
	"testing"

	"github.com/bluescreen10/pix"
	"github.com/bluescreen10/pix/colors"
	"github.com/bluescreen10/pix/glm"
	"github.com/bluescreen10/pix/scenes"
)

// The renderer's GPU draw state is cached per packet SOURCE, not held by the Scene.
// These tests pin the consequences: two scenes do not share state, and a released
// source rebuilds correctly rather than rendering from buffers nobody filled.

func greenCubeScene(t *testing.T, r *pix.Renderer) *scenes.Scene {
	t.Helper()
	scene := scenes.New()
	scene.SetAmbient(colors.RGB32F{1, 1, 1}, 1)
	geo := r.GeometryStore.Create(pix.BoxGeometry(1, 1, 1))
	mat := r.NewBasicMaterial()
	mat.SetColor(colors.RGBA32F{0, 1, 0, 1})
	scene.Add(scene.NewMesh(geo, mat))
	geo.Release()

	cam := scene.NewPerspectiveCamera(45, 1, 0.1, 100)
	scene.Add(cam)
	cam.SetPosition(glm.Vec3f{0, 0, 3})
	return scene
}

func greenPixels(px []byte) int {
	n := 0
	for i := 0; i+3 < len(px); i += 4 {
		if px[i+1] > 100 && px[i] < 60 {
			n++
		}
	}
	return n
}

// TestSourceCacheIsPerScene: each scene gets its own GPU draw state, keyed by its
// source id. Sharing one would mean the second scene drawn in a frame overwrote the
// first's drawable table — checked by re-rendering the first scene after the second
// and confirming it still reproduces its own frame, not the second scene's leftovers.
func TestSourceCacheIsPerScene(t *testing.T) {
	r, err := pix.NewOffscreenRenderer(64, 64)
	if err != nil {
		t.Fatal(err)
	}
	defer r.Destroy()

	a := greenCubeScene(t, r)
	defer a.Destroy()
	b := greenCubeScene(t, r)
	defer b.Destroy()

	if a.ID() == b.ID() {
		t.Fatal("two scenes minted the same source id")
	}

	r.Render(a)
	want := greenPixels(r.Pixels())
	if want == 0 {
		t.Fatal("scene a did not render at all")
	}

	r.Render(b)
	if got := greenPixels(r.Pixels()); got == 0 {
		t.Fatal("scene b did not render at all")
	}

	r.Render(a)
	if got := greenPixels(r.Pixels()); got != want {
		t.Errorf("re-rendering a after b produced %d green pixels, want %d — rendering b corrupted a's cached draw state", got, want)
	}
}

// TestReleasedSourceRendersAgain is the one that would break silently. Releasing a
// source throws away the buffers its transforms were uploaded into, but the producer
// tracks dirtiness since the last EXTRACTION and has no idea its consumer went away —
// so a fresh cache must upload regardless of what the packet says changed.
func TestReleasedSourceRendersAgain(t *testing.T) {
	r, err := pix.NewOffscreenRenderer(64, 64)
	if err != nil {
		t.Fatal(err)
	}
	defer r.Destroy()
	r.SetClearColor(colors.RGBA32F{0, 0, 0, 1})

	scene := greenCubeScene(t, r)
	defer scene.Destroy()

	r.Render(scene)
	before := greenPixels(r.Pixels())
	if before == 0 {
		t.Fatal("cube did not render at all")
	}

	r.ReleaseSource(scene.ID())

	// Nothing about the scene changed, so the packet reports no transform movement and
	// the same mesh revision. The renderer must still rebuild from the complete tables.
	r.Render(scene)
	if after := greenPixels(r.Pixels()); after != before {
		t.Errorf("after release and re-render: %d green pixels, want %d — the rebuilt "+
			"cache did not reproduce the frame", after, before)
	}
}

// TestReleaseSourceIsIdempotent: releasing an unknown or already-released id is a
// no-op, so a caller need not track whether it already did it — it must not panic,
// and the renderer must still work normally afterward.
func TestReleaseSourceIsIdempotent(t *testing.T) {
	r, err := pix.NewOffscreenRenderer(64, 64)
	if err != nil {
		t.Fatal(err)
	}
	defer r.Destroy()

	scene := greenCubeScene(t, r)
	defer scene.Destroy()
	r.Render(scene)

	r.ReleaseSource(scene.ID())
	r.ReleaseSource(scene.ID())
	r.ReleaseSource(scenes.NewSourceID())

	// The renderer must still work normally after releasing everything (including an
	// id it never saw).
	r.Render(scene)
	if greenPixels(r.Pixels()) == 0 {
		t.Error("renderer failed to render after a sequence of idempotent releases")
	}
}
