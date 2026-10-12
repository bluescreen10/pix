//go:build linux || windows

package render

// Register GameKit's Vulkan backend on desktop platforms without Metal.
import _ "github.com/bluescreen10/gamekit/gpu/vulkan"
