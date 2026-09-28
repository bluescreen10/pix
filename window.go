package pix

import (
	"github.com/bluescreen10/gamekit/gpu"
)

type swapchainSizer interface {
	SwapchainSize(gpu.Swapchain) (uint32, uint32)
}
