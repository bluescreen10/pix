//go:build darwin && gamekit_vulkan

package pix

// Vulkan on darwin (through MoltenVK or KosmicKrisp) is opt-in: build with
// -tags gamekit_vulkan to register it alongside Metal. It keeps its higher
// registration priority (see gpu.RegisterBackend in gamekit/gpu/vulkan), so with
// the tag set and no explicit RendererConfig.Backend/PIX_GPU_BACKEND, Vulkan is
// still the one auto-selected — the tag controls whether it's compiled in at all,
// not its priority once present.
import (
	_ "github.com/bluescreen10/gamekit/gpu/vulkan"
)
