//go:build darwin

package pix

// Register native Metal. It is the sole default backend on darwin — Vulkan (through
// MoltenVK or KosmicKrisp) is opt-in only, via the gamekit_vulkan build tag; see
// backend_darwin_vulkan.go. RendererConfig.Backend or PIX_GPU_BACKEND still selects
// explicitly among whatever is registered.
import (
	_ "github.com/bluescreen10/gamekit/gpu/metal"
)
