package pix_test

import (
	"fmt"
	"image"
	"os"
	"testing"

	"github.com/bluescreen10/gamekit/gpu"
)

// TestMain skips this package's tests when no gpu backend is registered for the
// platform — they all need a renderer, and gpu.Instance would otherwise panic.
// This keeps `go test ./...` green everywhere, running the tests only where a
// backend exists (e.g. Vulkan on macOS via backend_darwin.go).
func TestMain(m *testing.M) {
	if !gpu.HasBackend() {
		fmt.Println("pix: no gpu backend registered for this platform; skipping tests")
		os.Exit(0)
	}
	os.Exit(m.Run())
}

// nrgbaImage wraps raw RGBA bytes, w*h*4 of them, as an image, without copying them.
func nrgbaImage(pixels []byte, w, h int) *image.NRGBA {
	return &image.NRGBA{Pix: pixels, Stride: w * 4, Rect: image.Rect(0, 0, w, h)}
}
