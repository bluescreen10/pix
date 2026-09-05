package materials

import (
	"fmt"
	"os"
	"testing"

	"github.com/bluescreen10/pix/gpu"
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
func testStore(t *testing.T) (*Store, gpu.Backend) {
	t.Helper()
	backend := gpu.Instance(nil)
	if err := backend.Init(); err != nil {
		t.Fatal(err)
	}
	s := NewStore(backend)
	t.Cleanup(s.Destroy)
	return s, backend
}
