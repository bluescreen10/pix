package vulkan

/*
#include <vulkan/vulkan.h>
#include <stdint.h>
#include <stdlib.h>

// vkbSurfaceFromHandle reinterprets an opaque handle value (the uintptr the HAL
// passes across the package boundary) as a VkSurfaceKHR. Doing the cast in C keeps
// the Go side free of a vet-flagged uintptr->unsafe.Pointer conversion; the value
// is a real Vulkan handle, never Go memory.
static VkSurfaceKHR vkbSurfaceFromHandle(uint64_t h) { return (VkSurfaceKHR)h; }

// vkbCreateSwapchain creates (or recreates from old) a FIFO swapchain, picking a
// BGRA8/RGBA8 unorm format. Returns the swapchain, chosen format and extent.
static VkResult vkbCreateSwapchain(VkPhysicalDevice phys, VkDevice dev, VkSurfaceKHR surface,
                                   uint32_t w, uint32_t h, VkSwapchainKHR old,
                                   VkSwapchainKHR* outSwap, VkFormat* outFmt, uint32_t* outW, uint32_t* outH) {
    VkSurfaceCapabilitiesKHR caps;
    vkGetPhysicalDeviceSurfaceCapabilitiesKHR(phys, surface, &caps);

    uint32_t nf = 0;
    vkGetPhysicalDeviceSurfaceFormatsKHR(phys, surface, &nf, NULL);
    VkSurfaceFormatKHR* fmts = (VkSurfaceFormatKHR*)malloc(nf * sizeof(VkSurfaceFormatKHR));
    vkGetPhysicalDeviceSurfaceFormatsKHR(phys, surface, &nf, fmts);
    VkSurfaceFormatKHR chosen = fmts[0];
    for (uint32_t i = 0; i < nf; i++) {
        if ((fmts[i].format == VK_FORMAT_B8G8R8A8_UNORM || fmts[i].format == VK_FORMAT_R8G8B8A8_UNORM) &&
            fmts[i].colorSpace == VK_COLOR_SPACE_SRGB_NONLINEAR_KHR) { chosen = fmts[i]; break; }
    }
    free(fmts);

    VkExtent2D ext = caps.currentExtent;
    if (ext.width == 0xFFFFFFFF) { ext.width = w; ext.height = h; }

    uint32_t minImg = caps.minImageCount + 1;
    if (caps.maxImageCount > 0 && minImg > caps.maxImageCount) minImg = caps.maxImageCount;

    VkSwapchainCreateInfoKHR ci = {0};
    ci.sType = VK_STRUCTURE_TYPE_SWAPCHAIN_CREATE_INFO_KHR;
    ci.surface = surface;
    ci.minImageCount = minImg;
    ci.imageFormat = chosen.format;
    ci.imageColorSpace = chosen.colorSpace;
    ci.imageExtent = ext;
    ci.imageArrayLayers = 1;
    ci.imageUsage = VK_IMAGE_USAGE_COLOR_ATTACHMENT_BIT | VK_IMAGE_USAGE_TRANSFER_DST_BIT;
    ci.imageSharingMode = VK_SHARING_MODE_EXCLUSIVE;
    ci.preTransform = caps.currentTransform;
    ci.compositeAlpha = VK_COMPOSITE_ALPHA_OPAQUE_BIT_KHR;
    ci.presentMode = VK_PRESENT_MODE_FIFO_KHR;
    ci.clipped = VK_TRUE;
    ci.oldSwapchain = old;

    VkResult r = vkCreateSwapchainKHR(dev, &ci, NULL, outSwap);
    *outFmt = chosen.format;
    *outW = ext.width; *outH = ext.height;
    return r;
}

static uint32_t vkbSwapchainImageCount(VkDevice dev, VkSwapchainKHR swap) {
    uint32_t n = 0;
    vkGetSwapchainImagesKHR(dev, swap, &n, NULL);
    return n;
}

static void vkbSwapchainImages(VkDevice dev, VkSwapchainKHR swap, uint32_t n, VkImage* out) {
    vkGetSwapchainImagesKHR(dev, swap, &n, out);
}

static VkResult vkbSwapImageView(VkDevice dev, VkImage img, VkFormat fmt, VkImageView* out) {
    VkImageViewCreateInfo ci = {0};
    ci.sType = VK_STRUCTURE_TYPE_IMAGE_VIEW_CREATE_INFO;
    ci.image = img;
    ci.viewType = VK_IMAGE_VIEW_TYPE_2D;
    ci.format = fmt;
    ci.subresourceRange.aspectMask = VK_IMAGE_ASPECT_COLOR_BIT;
    ci.subresourceRange.levelCount = 1;
    ci.subresourceRange.layerCount = 1;
    return vkCreateImageView(dev, &ci, NULL, out);
}

static VkResult vkbCreateSemaphore(VkDevice dev, VkSemaphore* out) {
    VkSemaphoreCreateInfo ci = {0};
    ci.sType = VK_STRUCTURE_TYPE_SEMAPHORE_CREATE_INFO;
    return vkCreateSemaphore(dev, &ci, NULL, out);
}

static VkResult vkbAcquire(VkDevice dev, VkSwapchainKHR swap, VkSemaphore sem, uint32_t* outIndex) {
    return vkAcquireNextImageKHR(dev, swap, UINT64_MAX, sem, VK_NULL_HANDLE, outIndex);
}

// vkbPresentBarrier transitions a backbuffer from oldL to PRESENT_SRC.
static void vkbPresentBarrier(VkCommandBuffer cb, VkImage img, VkImageLayout oldL) {
    VkImageMemoryBarrier2 ib = {0};
    ib.sType = VK_STRUCTURE_TYPE_IMAGE_MEMORY_BARRIER_2;
    ib.srcStageMask = VK_PIPELINE_STAGE_2_COLOR_ATTACHMENT_OUTPUT_BIT;
    ib.srcAccessMask = VK_ACCESS_2_COLOR_ATTACHMENT_WRITE_BIT;
    ib.dstStageMask = VK_PIPELINE_STAGE_2_BOTTOM_OF_PIPE_BIT;
    ib.oldLayout = oldL;
    ib.newLayout = VK_IMAGE_LAYOUT_PRESENT_SRC_KHR;
    ib.image = img;
    ib.subresourceRange.aspectMask = VK_IMAGE_ASPECT_COLOR_BIT;
    ib.subresourceRange.levelCount = VK_REMAINING_MIP_LEVELS;
    ib.subresourceRange.layerCount = VK_REMAINING_ARRAY_LAYERS;
    VkDependencyInfo di = {0};
    di.sType = VK_STRUCTURE_TYPE_DEPENDENCY_INFO;
    di.imageMemoryBarrierCount = 1;
    di.pImageMemoryBarriers = &ib;
    vkCmdPipelineBarrier2(cb, &di);
}

// vkbSubmitPresent submits cb (waiting on acquire, signalling renderDone) then
// presents the image. Bundled so the semaphore wiring stays in C.
static VkResult vkbSubmitPresent(VkQueue q, VkCommandBuffer cb, VkSwapchainKHR swap, uint32_t imageIndex,
                                 VkSemaphore acquire, VkSemaphore renderDone, VkFence fence) {
    VkSemaphoreSubmitInfo wait = {0};
    wait.sType = VK_STRUCTURE_TYPE_SEMAPHORE_SUBMIT_INFO;
    wait.semaphore = acquire;
    wait.stageMask = VK_PIPELINE_STAGE_2_COLOR_ATTACHMENT_OUTPUT_BIT;

    VkSemaphoreSubmitInfo sig = {0};
    sig.sType = VK_STRUCTURE_TYPE_SEMAPHORE_SUBMIT_INFO;
    sig.semaphore = renderDone;
    sig.stageMask = VK_PIPELINE_STAGE_2_ALL_COMMANDS_BIT;

    VkCommandBufferSubmitInfo cbi = {0};
    cbi.sType = VK_STRUCTURE_TYPE_COMMAND_BUFFER_SUBMIT_INFO;
    cbi.commandBuffer = cb;

    VkSubmitInfo2 si = {0};
    si.sType = VK_STRUCTURE_TYPE_SUBMIT_INFO_2;
    si.waitSemaphoreInfoCount = 1;   si.pWaitSemaphoreInfos = &wait;
    si.commandBufferInfoCount = 1;   si.pCommandBufferInfos = &cbi;
    si.signalSemaphoreInfoCount = 1; si.pSignalSemaphoreInfos = &sig;
    VkResult r = vkQueueSubmit2(q, 1, &si, fence);
    if (r != VK_SUCCESS) return r;

    VkPresentInfoKHR pi = {0};
    pi.sType = VK_STRUCTURE_TYPE_PRESENT_INFO_KHR;
    pi.waitSemaphoreCount = 1;
    pi.pWaitSemaphores = &renderDone;
    pi.swapchainCount = 1;
    pi.pSwapchains = &swap;
    pi.pImageIndices = &imageIndex;
    return vkQueuePresentKHR(q, &pi);
}
*/
import "C"

import (
	"fmt"

	"github.com/bluescreen10/pix/gpu"
)

type swapchainState struct {
	surface  C.VkSurfaceKHR
	swap     C.VkSwapchainKHR
	format   C.VkFormat
	gpuFmt   gpu.Format
	w, h     uint32
	images   []gpu.Texture // one textureEntry per image (owned=false)
	acquire  []C.VkSemaphore
	rendered []C.VkSemaphore
	frame    uint32 // rotating sync index
	curImage uint32 // last acquired image index
}

func gpuFormatOf(f C.VkFormat) gpu.Format {
	if f == C.VK_FORMAT_R8G8B8A8_UNORM {
		return gpu.FormatRGBA8Unorm
	}
	return gpu.FormatBGRA8Unorm
}

// CreateSwapchain wraps a platform VkSurfaceKHR (surface is the uintptr returned
// by e.g. glfw CreateWindowSurface) in a swapchain, surfacing backbuffers as
// render-target Textures.
func (b *Backend) CreateSwapchain(surface uintptr, width, height uint32) gpu.Swapchain {
	s := &swapchainState{surface: C.vkbSurfaceFromHandle(C.uint64_t(surface))}
	b.buildSwapchain(s, width, height, nil)

	h := b.nextH
	b.nextH++
	b.swapchains[h] = s
	return gpu.Swapchain{H: gpu.Handle(h)}
}

func (b *Backend) buildSwapchain(s *swapchainState, width, height uint32, old C.VkSwapchainKHR) {
	var swap C.VkSwapchainKHR
	var cfmt C.VkFormat
	var w, h C.uint32_t
	if r := C.vkbCreateSwapchain(b.phys, b.device, s.surface, C.uint32_t(width), C.uint32_t(height), old,
		&swap, &cfmt, &w, &h); r != C.VK_SUCCESS {
		panic(fmt.Sprintf("vulkan: swapchain creation failed (%d)", int(r)))
	}
	s.swap = swap
	s.format = cfmt
	s.gpuFmt = gpuFormatOf(cfmt)
	s.w, s.h = uint32(w), uint32(h)

	n := uint32(C.vkbSwapchainImageCount(b.device, swap))
	imgs := make([]C.VkImage, n)
	C.vkbSwapchainImages(b.device, swap, C.uint32_t(n), &imgs[0])

	s.images = make([]gpu.Texture, n)
	s.acquire = make([]C.VkSemaphore, n)
	s.rendered = make([]C.VkSemaphore, n)
	for i := uint32(0); i < n; i++ {
		var view C.VkImageView
		C.vkbSwapImageView(b.device, imgs[i], cfmt, &view)
		hh := b.nextH
		b.nextH++
		b.textures[hh] = &textureEntry{
			img: imgs[i], view: view, format: cfmt,
			width: s.w, height: s.h, layout: C.VK_IMAGE_LAYOUT_UNDEFINED, owned: false,
		}
		s.images[i] = gpu.Texture{H: gpu.Handle(hh)}
		C.vkbCreateSemaphore(b.device, &s.acquire[i])
		C.vkbCreateSemaphore(b.device, &s.rendered[i])
	}
}

// AcquireNext acquires the next backbuffer and returns it as a render-target
// Texture. The returned Fence is unused (present sync is via internal semaphores;
// CPU throttling is via WaitIdle for now).
func (b *Backend) AcquireNext(sc gpu.Swapchain) (gpu.Texture, gpu.Fence) {
	s := b.swapchains[uint64(sc.H)]
	s.frame = (s.frame + 1) % uint32(len(s.acquire))
	var idx C.uint32_t
	C.vkbAcquire(b.device, s.swap, s.acquire[s.frame], &idx)
	s.curImage = uint32(idx)
	b.activeSwap = s
	// Fresh acquire: the image's prior contents are undefined for our purposes.
	b.tex(s.images[s.curImage]).layout = C.VK_IMAGE_LAYOUT_UNDEFINED
	return s.images[s.curImage], gpu.Fence{}
}

// Present ends and submits the recorded command list (waiting on the acquire
// semaphore, signalling render-done) and presents the backbuffer. The caller
// must have recorded rendering into the Texture returned by AcquireNext.
func (b *Backend) Present(sc gpu.Swapchain, cl gpu.CommandList) {
	s := b.swapchains[uint64(sc.H)]
	c := cl.(*cmdList)

	// Transition the backbuffer to PRESENT_SRC before ending the buffer.
	e := b.tex(s.images[s.curImage])
	C.vkbPresentBarrier(c.cb, e.img, e.layout)
	e.layout = C.VK_IMAGE_LAYOUT_PRESENT_SRC_KHR
	C.vkEndCommandBuffer(c.cb)

	var noFence C.VkFence
	C.vkbSubmitPresent(b.queue, c.cb, s.swap, C.uint32_t(s.curImage),
		s.acquire[s.frame], s.rendered[s.frame], noFence)

	// Simple throttle (per-frame fences / frames-in-flight come later).
	C.vkQueueWaitIdle(b.queue)
	C.vkFreeCommandBuffers(b.device, b.cmdPool, 1, &c.cb)
	b.activeSwap = nil
}

// SwapchainFormat returns the swapchain's color format.
func (b *Backend) SwapchainFormat(sc gpu.Swapchain) gpu.Format {
	return b.swapchains[uint64(sc.H)].gpuFmt
}

// SwapchainSize returns the swapchain's actual backbuffer extent (which may
// differ from a window's framebuffer size on HiDPI). Use it for the viewport.
func (b *Backend) SwapchainSize(sc gpu.Swapchain) (width, height uint32) {
	s := b.swapchains[uint64(sc.H)]
	return s.w, s.h
}

// ResizeSwapchain recreates the swapchain at a new size.
func (b *Backend) ResizeSwapchain(sc gpu.Swapchain, width, height uint32) {
	s := b.swapchains[uint64(sc.H)]
	C.vkDeviceWaitIdle(b.device)
	b.destroySwapchainResources(s)
	b.buildSwapchain(s, width, height, s.swap)
}

func (b *Backend) destroySwapchainResources(s *swapchainState) {
	for i := range s.images {
		e := b.tex(s.images[i])
		if e != nil {
			C.vkDestroyImageView(b.device, e.view, nil)
			delete(b.textures, uint64(s.images[i].H))
		}
		C.vkDestroySemaphore(b.device, s.acquire[i], nil)
		C.vkDestroySemaphore(b.device, s.rendered[i], nil)
	}
	C.vkDestroySwapchainKHR(b.device, s.swap, nil)
}

func (b *Backend) destroyAllSwapchains() {
	for _, s := range b.swapchains {
		b.destroySwapchainResources(s)
		C.vkDestroySurfaceKHR(b.instance, s.surface, nil)
	}
	b.swapchains = map[uint64]*swapchainState{}
}
