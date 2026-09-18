package pix

import (
	"testing"

	"github.com/bluescreen10/pix/cameras"
	"github.com/bluescreen10/pix/colors"
	"github.com/bluescreen10/pix/glm"
	"github.com/bluescreen10/pix/scenes"
)

// The renderer's GPU draw state is cached per packet SOURCE, not held by the Scene.
// These tests pin the consequences: two scenes do not share state, a released source
// rebuilds correctly rather than rendering from buffers nobody filled, and the cache
// does not grow without bound.

func greenCubeScene(t *testing.T, r *Renderer) (*scenes.Scene, Camera) {
	t.Helper()
	scene := scenes.New()
	scene.SetAmbient(colors.RGB32F{1, 1, 1})
	geo := r.GeometryStore.Create(BoxGeometry(1, 1, 1))
	mat := r.NewBasicMaterial()
	mat.SetColor(colors.RGBA32F{0, 1, 0, 1})
	scene.Add(scene.NewMesh(geo, mat))
	geo.Release()

	cam := cameras.NewPerspectiveCamera(45, 1, 0.1, 100)
	cam.SetPosition(glm.Vec3f{0, 0, 3})
	return scene, cam
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
// first's drawable table.
func TestSourceCacheIsPerScene(t *testing.T) {
	r, err := NewOffscreenRenderer(64, 64)
	if err != nil {
		t.Fatal(err)
	}
	defer r.Destroy()

	a, camA := greenCubeScene(t, r)
	defer a.Destroy()
	b, _ := greenCubeScene(t, r)
	defer b.Destroy()

	if a.ID() == b.ID() {
		t.Fatal("two scenes minted the same source id")
	}
	r.Render(a, camA)
	r.Render(b, camA)

	if len(r.sources) != 2 {
		t.Fatalf("renderer cached %d sources, want 2", len(r.sources))
	}
	if r.stateFor(a.ID()) == r.stateFor(b.ID()) {
		t.Error("both scenes resolved to the same render state")
	}
}

// TestReleasedSourceRendersAgain is the one that would break silently. Releasing a
// source throws away the buffers its transforms were uploaded into, but the producer
// tracks dirtiness since the last EXTRACTION and has no idea its consumer went away —
// so a fresh cache must upload regardless of what the packet says changed.
func TestReleasedSourceRendersAgain(t *testing.T) {
	r, err := NewOffscreenRenderer(64, 64)
	if err != nil {
		t.Fatal(err)
	}
	defer r.Destroy()
	r.SetClearColor(colors.RGBA32F{0, 0, 0, 1})

	scene, cam := greenCubeScene(t, r)
	defer scene.Destroy()

	r.Render(scene, cam)
	before := greenPixels(r.Pixels())
	if before == 0 {
		t.Fatal("cube did not render at all")
	}

	r.ReleaseSource(scene.ID())
	if len(r.sources) != 0 {
		t.Fatalf("ReleaseSource left %d sources cached", len(r.sources))
	}

	// Nothing about the scene changed, so the packet reports no transform movement and
	// the same mesh revision. The renderer must still rebuild from the complete tables.
	r.Render(scene, cam)
	if after := greenPixels(r.Pixels()); after != before {
		t.Errorf("after release and re-render: %d green pixels, want %d — the rebuilt "+
			"cache did not reproduce the frame", after, before)
	}
}

// TestReleaseSourceIsIdempotent: releasing an unknown or already-released id is a
// no-op, so a caller need not track whether it already did it.
func TestReleaseSourceIsIdempotent(t *testing.T) {
	r, err := NewOffscreenRenderer(64, 64)
	if err != nil {
		t.Fatal(err)
	}
	defer r.Destroy()

	scene, cam := greenCubeScene(t, r)
	defer scene.Destroy()
	r.Render(scene, cam)

	r.ReleaseSource(scene.ID())
	r.ReleaseSource(scene.ID())
	r.ReleaseSource(scenes.NewSourceID())
	if len(r.sources) != 0 {
		t.Errorf("renderer holds %d sources after releasing everything", len(r.sources))
	}
}
