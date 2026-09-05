package pix

import (
	"image/png"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/bluescreen10/pix/cameras"
	"github.com/bluescreen10/pix/colors"
	"github.com/bluescreen10/pix/glm"
)

// shotScene builds a renderer showing one lit cube, so a capture has something in it
// that is neither uniform nor the clear colour.
func shotScene(t *testing.T, w, h uint32) (*Renderer, *Scene, Camera) {
	t.Helper()
	r, err := NewOffscreenRenderer(w, h)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(r.Destroy)
	r.SetClearColor(colors.RGBA32F{0, 0, 0, 1})

	scene := r.NewScene()
	t.Cleanup(scene.Destroy)
	scene.SetAmbient(colors.RGB32F{0.3, 0.3, 0.3})
	scene.AddDirectionalLight(glm.Vec3f{-0.4, -1, -0.3}, colors.RGB32F{1, 1, 1}, 2)

	cube := r.GeometryStore.Create(normalCube())
	t.Cleanup(cube.Release)
	mat := r.NewPBRMaterial()
	mat.SetColor(colors.RGBA32F{0.9, 0.2, 0.2, 1})
	scene.Add(scene.NewMesh(cube, mat))

	cam := cameras.NewPerspectiveCamera(45, 1, 0.1, 100)
	cam.SetPosition(glm.Vec3f{0, 0, 3})
	return r, scene, cam
}

// TestScreenshotWritesTheRenderedFrame is the whole feature: the file must appear, be a
// readable PNG of the right size, and contain the frame rather than a blank image.
func TestScreenshotWritesTheRenderedFrame(t *testing.T) {
	const w, h = 96, 96
	r, scene, cam := shotScene(t, w, h)
	path := filepath.Join(t.TempDir(), "shot.png")

	var gotPath string
	var gotErr error
	called := false
	r.Screenshot(path, func(p string, err error) {
		gotPath, gotErr, called = p, err, true
	})

	r.Render(scene, cam) // the capture rides this frame

	if !called {
		t.Fatal("the callback never fired — the capture did not run during Render")
	}
	if gotErr != nil {
		t.Fatalf("screenshot failed: %v", gotErr)
	}
	if gotPath != path {
		t.Fatalf("callback reported %q, want %q", gotPath, path)
	}

	f, err := os.Open(path)
	if err != nil {
		t.Fatalf("no file was written: %v", err)
	}
	defer f.Close()
	img, err := png.Decode(f)
	if err != nil {
		t.Fatalf("the file is not a readable PNG: %v", err)
	}
	if b := img.Bounds(); b.Dx() != w || b.Dy() != h {
		t.Fatalf("image is %dx%d, want %dx%d", b.Dx(), b.Dy(), w, h)
	}

	// It must be the frame, not an empty buffer: the cube is red on black, so some
	// pixel has to be lit, and every pixel must be opaque.
	var lit int
	for y := range h {
		for x := range w {
			cr, _, _, ca := img.At(x, y).RGBA()
			if ca != 0xFFFF {
				t.Fatalf("pixel (%d,%d) has alpha %d, want opaque", x, y, ca>>8)
			}
			if cr>>8 > 40 {
				lit++
			}
		}
	}
	if lit == 0 {
		t.Fatal("the capture is blank — no lit pixels from the cube")
	}
}

// TestScreenshotDefaultsToATimestampedName: `screenshot` with no argument has to go
// somewhere predictable rather than failing.
func TestScreenshotDefaultsToATimestampedName(t *testing.T) {
	r, scene, cam := shotScene(t, 32, 32)

	dir := t.TempDir()
	wd, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Chdir(dir); err != nil {
		t.Fatal(err)
	}
	defer os.Chdir(wd)

	var gotPath string
	r.Screenshot("", func(p string, err error) {
		if err != nil {
			t.Errorf("screenshot failed: %v", err)
		}
		gotPath = p
	})
	r.Render(scene, cam)

	if !strings.HasPrefix(gotPath, "pix-") || !strings.HasSuffix(gotPath, ".png") {
		t.Fatalf("default name = %q, want a pix-*.png", gotPath)
	}
	if _, err := os.Stat(filepath.Join(dir, gotPath)); err != nil {
		t.Fatalf("default-named file missing: %v", err)
	}
}

// TestScreenshotCreatesMissingDirectories: a path into a folder that does not exist yet
// should just work, rather than making the user mkdir first.
func TestScreenshotCreatesMissingDirectories(t *testing.T) {
	r, scene, cam := shotScene(t, 32, 32)
	path := filepath.Join(t.TempDir(), "shots", "nested", "a.png")

	var gotErr error
	r.Screenshot(path, func(_ string, err error) { gotErr = err })
	r.Render(scene, cam)

	if gotErr != nil {
		t.Fatalf("screenshot into a new directory failed: %v", gotErr)
	}
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("file missing: %v", err)
	}
}

// TestScreenshotIsOneShot: a queued capture must not repeat on later frames, which
// would rewrite the file every frame forever.
func TestScreenshotIsOneShot(t *testing.T) {
	r, scene, cam := shotScene(t, 32, 32)
	path := filepath.Join(t.TempDir(), "once.png")

	calls := 0
	r.Screenshot(path, func(string, error) { calls++ })
	r.Render(scene, cam)
	r.Render(scene, cam)
	r.Render(scene, cam)

	if calls != 1 {
		t.Fatalf("callback fired %d times, want exactly 1", calls)
	}
}
