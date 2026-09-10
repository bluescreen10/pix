package pix

import (
	"fmt"

	"github.com/bluescreen10/gamekit"
	"github.com/bluescreen10/gamekit/gpu"
	"github.com/bluescreen10/pix/colors"
)

type swapchainSizer interface {
	SwapchainSize(gpu.Swapchain) (uint32, uint32)
}

// attachWindow lets the windowing package own platform handles and surface
// creation. Pix only needs the resulting opaque surface and swapchain extent.
func (r *Renderer) attachWindow(w *gamekit.Window, width, height uint32) error {
	surface, err := w.CreateSurface(r.backend)
	if err != nil {
		return err
	}
	r.swapchain = r.backend.CreateSwapchain(surface, width, height)
	sizer, ok := r.backend.(swapchainSizer)
	if !ok {
		return fmt.Errorf("backend does not expose swapchain size")
	}
	sw, sh := sizer.SwapchainSize(r.swapchain)
	r.hasTarget = false
	r.clear = colors.RGBA32F{}
	r.configure(sw, sh, r.backend.SwapchainFormat(r.swapchain))
	return nil
}
