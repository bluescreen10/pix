//go:build darwin

package pix

// On macOS the default GPU backend is Vulkan (via KosmicKrisp). Registering it is a
// blank import in this build-tagged file so the renderer core never references a
// concrete backend — swap this file (or add a build tag) when a native Metal backend
// lands, and platforms without Vulkan simply don't compile it.
import _ "github.com/bluescreen10/pix/gpu/vulkan"
