package materials_test

import (
	"fmt"
	"image"
	"os"
	"testing"

	"github.com/bluescreen10/gamekit/gpu"
	"github.com/bluescreen10/pix/materials"
)

// TestMain skips this package's tests when no gpu backend is registered for the
// platform — a Pool allocates its record buffer up front, so gpu.Instance would
// otherwise panic. Mirrors the same guard in package pix.
func TestMain(m *testing.M) {
	if !gpu.HasBackend() {
		fmt.Println("materials: no gpu backend registered for this platform; skipping tests")
		os.Exit(0)
	}
	os.Exit(m.Run())
}

// testStore returns a Store on a freshly initialized backend, plus that backend for
// tests that need to record commands of their own.
func testStore(t *testing.T) (*materials.Store, gpu.Backend) {
	t.Helper()
	backend := gpu.Instance(nil)
	if err := backend.Init(); err != nil {
		t.Fatal(err)
	}
	s := materials.NewStore(backend, 0)
	t.Cleanup(s.Destroy)
	return s, backend
}

// nrgbaImage wraps raw RGBA bytes, w*h*4 of them, as an image, without copying them.
func nrgbaImage(pixels []byte, w, h int) *image.NRGBA {
	return &image.NRGBA{Pix: pixels, Stride: w * 4, Rect: image.Rect(0, 0, w, h)}
}
