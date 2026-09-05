//go:build darwin

package materials

// On macOS the default GPU backend is Vulkan (via KosmicKrisp). Registering it is a
// blank import in this build-tagged test file so the package itself never references
// a concrete backend — the same arrangement as backend_darwin.go in package pix.
import _ "github.com/bluescreen10/pix/gpu/vulkan"
