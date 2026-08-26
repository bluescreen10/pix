package vulkan

// #include <vulkan/vulkan.h>
import "C"

import "github.com/bluescreen10/pix/gpu"

// vkFormat maps an gpu.Format to a VkFormat.
func vkFormat(f gpu.Format) C.VkFormat {
	switch f {
	case gpu.FormatR8Unorm:
		return C.VK_FORMAT_R8_UNORM
	case gpu.FormatRGBA8Unorm:
		return C.VK_FORMAT_R8G8B8A8_UNORM
	case gpu.FormatRGBA8Srgb:
		return C.VK_FORMAT_R8G8B8A8_SRGB
	case gpu.FormatBGRA8Unorm:
		return C.VK_FORMAT_B8G8R8A8_UNORM
	case gpu.FormatBGRA8Srgb:
		return C.VK_FORMAT_B8G8R8A8_SRGB
	case gpu.FormatRG16F:
		return C.VK_FORMAT_R16G16_SFLOAT
	case gpu.FormatRGBA16F:
		return C.VK_FORMAT_R16G16B16A16_SFLOAT
	case gpu.FormatR32F:
		return C.VK_FORMAT_R32_SFLOAT
	case gpu.FormatRGBA32F:
		return C.VK_FORMAT_R32G32B32A32_SFLOAT
	case gpu.FormatRGB10A2Unorm:
		return C.VK_FORMAT_A2B10G10R10_UNORM_PACK32
	case gpu.FormatDepth32F:
		return C.VK_FORMAT_D32_SFLOAT
	case gpu.FormatDepth24Stencil8:
		return C.VK_FORMAT_D24_UNORM_S8_UINT
	default:
		return C.VK_FORMAT_UNDEFINED
	}
}
