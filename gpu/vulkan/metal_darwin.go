//go:build darwin

package vulkan

/*
#cgo LDFLAGS: -framework Cocoa -framework QuartzCore
#define VK_USE_PLATFORM_METAL_EXT
#include <vulkan/vulkan.h>

extern void* pixSetupMetalLayer(void* nsWindow);

static VkResult vkbCreateMetalSurface(VkInstance inst, void* nsWindow, VkSurfaceKHR* out) {
    void* layer = pixSetupMetalLayer(nsWindow);
    VkMetalSurfaceCreateInfoEXT ci = {0};
    ci.sType = VK_STRUCTURE_TYPE_METAL_SURFACE_CREATE_INFO_EXT;
    ci.pLayer = (const CAMetalLayer*)layer;
    return vkCreateMetalSurfaceEXT(inst, &ci, NULL, out);
}
*/
import "C"

import (
	"fmt"
	"unsafe"
)

// MetalSurfaceExtensions are the instance extensions needed for a Metal surface;
// pass them to New when creating an on-screen backend on macOS.
var MetalSurfaceExtensions = []string{"VK_KHR_surface", "VK_EXT_metal_surface"}

// CreateMetalSurface makes a VkSurfaceKHR from a Cocoa NSWindow (e.g. glfw
// Window.GetCocoaWindow()), returning it as a uintptr for CreateSwapchain. The
// window's content view is made CAMetalLayer-backed. Instance must have been
// created with MetalSurfaceExtensions.
func (b *Backend) CreateMetalSurface(nsWindow unsafe.Pointer) uintptr {
	var surf C.VkSurfaceKHR
	if r := C.vkbCreateMetalSurface(b.instance, nsWindow, &surf); r != C.VK_SUCCESS {
		panic(fmt.Sprintf("vulkan: vkCreateMetalSurfaceEXT failed (%d)", int(r)))
	}
	return uintptr(unsafe.Pointer(surf))
}
