package pix

import (
	"fmt"
	"os"
	"testing"

	"github.com/bluescreen10/pix/gpu"
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
