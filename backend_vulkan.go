//go:build linux || windows

package pix

// Register GameKit's Vulkan backend on desktop platforms without Metal.
import _ "github.com/bluescreen10/gamekit/gpu/vulkan"
