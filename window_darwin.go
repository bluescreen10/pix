package pix

import (
	"unsafe"

	"github.com/bluescreen10/pix/gpu"
)

// Window is the macOS platform window handle carried in RendererConfig.
type Window struct {
	// NSWindow is the Cocoa window pointer (e.g. glfw Window.GetCocoaWindow()).
	NSWindow unsafe.Pointer
}

// metalSurfacer is the backend capability the macOS windowed path needs. The Vulkan
// backend (and a future native Metal backend) implement it; the renderer reaches it
// by assertion so it never imports a concrete backend.
type metalSurfacer interface {
	CreateMetalSurface(nsWindow unsafe.Pointer) uintptr
	SwapchainSize(sc gpu.Swapchain) (uint32, uint32)
}

// attachWindow builds a Metal surface + swapchain for the window and configures the
// renderer (depth + pipelines) to the swapchain's real extent. width/height are an
// extent hint (the swapchain's currentExtent is authoritative).
func (r *Renderer) attachWindow(w *Window, width, height uint32) {
	ms := r.backend.(metalSurfacer)
	surface := ms.CreateMetalSurface(w.NSWindow)
	r.sc = r.backend.CreateSwapchain(surface, width, height)
	sw, sh := ms.SwapchainSize(r.sc)
	r.hasTarget = false
	r.clear = [4]float32{0.05, 0.06, 0.1, 1}
	r.configure(sw, sh, r.backend.SwapchainFormat(r.sc))
}
