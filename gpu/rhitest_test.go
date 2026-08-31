package gpu_test

// Backend-agnostic RHI conformance tests. They exercise the gpu.Backend interface
// (buffers, pipelines, dynamic rendering, compute→indirect, bindless textures) and
// so validate ANY backend, not just Vulkan. The backend is obtained from the
// registry and the suite skips when none is registered/initializable — so
// `go test ./...` passes on every platform, running the tests only where a backend
// exists. Register a backend for this suite with a build-tagged blank import (see
// backend_darwin_test.go).

import (
	_ "embed"
	"sync"
	"testing"

	"github.com/bluescreen10/pix/gpu"
)

// Test shaders are fixtures under gpu/testdata, embedded here (a _test.go file) so
// they never enter the production shaders package. SPIR-V today; add a backend's
// own compiled variants here when a non-SPIR-V backend lands.
var (
	//go:embed testdata/triangle.vert.spv
	triangleVert []byte
	//go:embed testdata/triangle.frag.spv
	triangleFrag []byte
	//go:embed testdata/mesh.vert.spv
	meshVert []byte
	//go:embed testdata/mesh.frag.spv
	meshFrag []byte
	//go:embed testdata/instanced.comp.spv
	instancedComp []byte
	//go:embed testdata/instanced.vert.spv
	instancedVert []byte
	//go:embed testdata/fill_indirect.comp.spv
	fillIndirect []byte
	//go:embed testdata/textured.vert.spv
	texturedVert []byte
	//go:embed testdata/textured.frag.spv
	texturedFrag []byte
)

var (
	backendOnce sync.Once
	backendInst gpu.Backend
	backendErr  error
)

// testBackend returns the registered gpu backend, initialized once and shared
// across the conformance tests. It skips (never fails) when no backend is
// registered for the platform or the device can't be created, so the suite is
// safe to run anywhere. Select a specific backend with gpu.Lookup(name) if more
// than one is ever registered.
func testBackend(t *testing.T) gpu.Backend {
	t.Helper()
	if !gpu.HasBackend() {
		t.Skip("no gpu backend registered for this platform")
	}
	backendOnce.Do(func() {
		backendInst = gpu.Instance(nil)
		backendErr = backendInst.Init()
	})
	if backendErr != nil {
		t.Skipf("gpu backend init failed (no device?): %v", backendErr)
	}
	return backendInst
}
