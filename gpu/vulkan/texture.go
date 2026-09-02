package vulkan

/*
#include <vulkan/vulkan.h>
#include <stdint.h>

// vkbCreateImage creates an image + dedicated device-local memory + a default
// view. usage is a VkImageUsageFlags mask assembled on the Go side.
static VkResult vkbCreateImage(VkDevice dev, VkPhysicalDevice phys,
                               VkFormat fmt, uint32_t w, uint32_t h, uint32_t layers, uint32_t mips,
                               VkImageUsageFlags usage, VkImageAspectFlags aspect, VkImageViewType viewType,
                               VkImage* outImg, VkDeviceMemory* outMem, VkImageView* outView) {
    VkImageCreateInfo ci = {0};
    ci.sType = VK_STRUCTURE_TYPE_IMAGE_CREATE_INFO;
    ci.imageType = VK_IMAGE_TYPE_2D;
    ci.format = fmt;
    ci.extent.width = w; ci.extent.height = h; ci.extent.depth = 1;
    ci.mipLevels = mips;
    ci.arrayLayers = layers;
    ci.samples = VK_SAMPLE_COUNT_1_BIT;
    ci.tiling = VK_IMAGE_TILING_OPTIMAL;
    ci.usage = usage;
    ci.sharingMode = VK_SHARING_MODE_EXCLUSIVE;
    ci.initialLayout = VK_IMAGE_LAYOUT_UNDEFINED;
    if (viewType == VK_IMAGE_VIEW_TYPE_CUBE || viewType == VK_IMAGE_VIEW_TYPE_CUBE_ARRAY)
        ci.flags |= VK_IMAGE_CREATE_CUBE_COMPATIBLE_BIT;

    VkResult r = vkCreateImage(dev, &ci, NULL, outImg);
    if (r != VK_SUCCESS) return r;

    VkMemoryRequirements req;
    vkGetImageMemoryRequirements(dev, *outImg, &req);
    VkPhysicalDeviceMemoryProperties mp;
    vkGetPhysicalDeviceMemoryProperties(phys, &mp);
    uint32_t ti = 0xFFFFFFFF;
    for (uint32_t i = 0; i < mp.memoryTypeCount; i++) {
        if ((req.memoryTypeBits & (1u << i)) &&
            (mp.memoryTypes[i].propertyFlags & VK_MEMORY_PROPERTY_DEVICE_LOCAL_BIT)) { ti = i; break; }
    }
    if (ti == 0xFFFFFFFF) { vkDestroyImage(dev, *outImg, NULL); return VK_ERROR_OUT_OF_DEVICE_MEMORY; }

    VkMemoryAllocateInfo ai = {0};
    ai.sType = VK_STRUCTURE_TYPE_MEMORY_ALLOCATE_INFO;
    ai.allocationSize = req.size;
    ai.memoryTypeIndex = ti;
    r = vkAllocateMemory(dev, &ai, NULL, outMem);
    if (r != VK_SUCCESS) { vkDestroyImage(dev, *outImg, NULL); return r; }
    r = vkBindImageMemory(dev, *outImg, *outMem, 0);
    if (r != VK_SUCCESS) { vkFreeMemory(dev, *outMem, NULL); vkDestroyImage(dev, *outImg, NULL); return r; }

    VkImageViewCreateInfo vci = {0};
    vci.sType = VK_STRUCTURE_TYPE_IMAGE_VIEW_CREATE_INFO;
    vci.image = *outImg;
    vci.viewType = viewType;
    vci.format = fmt;
    vci.subresourceRange.aspectMask = aspect;
    vci.subresourceRange.levelCount = mips;
    vci.subresourceRange.layerCount = layers;
    r = vkCreateImageView(dev, &vci, NULL, outView);
    if (r != VK_SUCCESS) { vkFreeMemory(dev, *outMem, NULL); vkDestroyImage(dev, *outImg, NULL); return r; }
    return VK_SUCCESS;
}

// vkbWriteSampledImage registers a view into the bindless sampled-image array. layout
// is the layout the image is guaranteed to be in whenever this descriptor is used —
// SHADER_READ_ONLY_OPTIMAL for color, DEPTH_STENCIL_READ_ONLY_OPTIMAL for depth (which
// additionally permits the image to be bound as a read-only depth attachment in the
// same render pass, so the deferred lighting pass can depth-test against the very
// buffer it samples).
static void vkbWriteSampledImage(VkDevice dev, VkDescriptorSet set, uint32_t index, VkImageView view,
                                 VkImageLayout layout) {
    VkDescriptorImageInfo ii = {0};
    ii.imageView = view;
    ii.imageLayout = layout;
    VkWriteDescriptorSet w = {0};
    w.sType = VK_STRUCTURE_TYPE_WRITE_DESCRIPTOR_SET;
    w.dstSet = set;
    w.dstBinding = 0; // BIND_SAMPLED
    w.dstArrayElement = index;
    w.descriptorCount = 1;
    w.descriptorType = VK_DESCRIPTOR_TYPE_SAMPLED_IMAGE;
    w.pImageInfo = &ii;
    vkUpdateDescriptorSets(dev, 1, &w, 0, NULL);
}

static void vkbWriteStorageImage(VkDevice dev, VkDescriptorSet set, uint32_t index, VkImageView view) {
    VkDescriptorImageInfo ii = {0};
    ii.imageView = view;
    ii.imageLayout = VK_IMAGE_LAYOUT_GENERAL;
    VkWriteDescriptorSet w = {0};
    w.sType = VK_STRUCTURE_TYPE_WRITE_DESCRIPTOR_SET;
    w.dstSet = set;
    w.dstBinding = 1; // BIND_STORAGE
    w.dstArrayElement = index;
    w.descriptorCount = 1;
    w.descriptorType = VK_DESCRIPTOR_TYPE_STORAGE_IMAGE;
    w.pImageInfo = &ii;
    vkUpdateDescriptorSets(dev, 1, &w, 0, NULL);
}

// vkbCreateSubView makes an additional view over a subresource range of an
// existing image (e.g. one array layer or mip), for registering into the heap.
static VkResult vkbCreateSubView(VkDevice dev, VkImage img, VkFormat fmt, VkImageAspectFlags aspect,
                                 VkImageViewType viewType, uint32_t baseMip, uint32_t mipCount,
                                 uint32_t baseLayer, uint32_t layerCount, VkImageView* outView) {
    VkImageViewCreateInfo vci = {0};
    vci.sType = VK_STRUCTURE_TYPE_IMAGE_VIEW_CREATE_INFO;
    vci.image = img;
    vci.viewType = viewType;
    vci.format = fmt;
    vci.subresourceRange.aspectMask = aspect;
    vci.subresourceRange.baseMipLevel = baseMip;
    vci.subresourceRange.levelCount = mipCount;
    vci.subresourceRange.baseArrayLayer = baseLayer;
    vci.subresourceRange.layerCount = layerCount;
    return vkCreateImageView(dev, &vci, NULL, outView);
}

static void vkbDestroyImage(VkDevice dev, VkImage img, VkDeviceMemory mem, VkImageView view) {
    vkDestroyImageView(dev, view, NULL);
    vkFreeMemory(dev, mem, NULL);
    vkDestroyImage(dev, img, NULL);
}
*/
import "C"

import (
	"fmt"

	"github.com/bluescreen10/pix/gpu"
)

// textureEntry is the backend record for a Texture handle. layout tracks the
// image's current Vulkan layout so the backend can transition it internally
// (the gpu API hides layouts, but Vulkan still requires them).
//
// lastStage/lastAccess track how the image was last used, so a transition can name
// the real producer as its source scope. Without this, a barrier that guesses
// TOP_OF_PIPE establishes no dependency on the previous pass's writes, and reads or
// re-writes of the same image across render passes race (Vulkan does not order
// separate render pass instances implicitly).
type textureEntry struct {
	img        C.VkImage
	mem        C.VkDeviceMemory
	view       C.VkImageView
	format     C.VkFormat
	width      uint32
	height     uint32
	layout     C.VkImageLayout
	lastStage  C.VkPipelineStageFlags2
	lastAccess C.VkAccessFlags2
	depth      bool
	// swapchain-owned backbuffers set owned=false so Destroy skips image/mem.
	owned bool
}

// sampledLayout is the layout a texture rests in while it's read through the bindless
// heap. Depth images use DEPTH_STENCIL_READ_ONLY_OPTIMAL so they may simultaneously be
// bound as a read-only depth attachment (see PrepareSampled / BeginRenderPass).
func sampledLayout(depth bool) C.VkImageLayout {
	if depth {
		return C.VK_IMAGE_LAYOUT_DEPTH_STENCIL_READ_ONLY_OPTIMAL
	}
	return C.VK_IMAGE_LAYOUT_SHADER_READ_ONLY_OPTIMAL
}

func viewType(k gpu.TextureKind) C.VkImageViewType {
	switch k {
	case gpu.Texture2DArray:
		return C.VK_IMAGE_VIEW_TYPE_2D_ARRAY
	case gpu.TextureCube:
		return C.VK_IMAGE_VIEW_TYPE_CUBE
	case gpu.TextureCubeArray:
		return C.VK_IMAGE_VIEW_TYPE_CUBE_ARRAY
	case gpu.Texture3D:
		return C.VK_IMAGE_VIEW_TYPE_3D
	default:
		return C.VK_IMAGE_VIEW_TYPE_2D
	}
}

func imageUsage(u gpu.TextureUsage, depth bool) C.VkImageUsageFlags {
	var f C.VkImageUsageFlags
	if u&gpu.TextureSampled != 0 {
		f |= C.VK_IMAGE_USAGE_SAMPLED_BIT
	}
	if u&gpu.TextureStorage != 0 {
		f |= C.VK_IMAGE_USAGE_STORAGE_BIT
	}
	if u&gpu.TextureRenderTarget != 0 {
		f |= C.VK_IMAGE_USAGE_COLOR_ATTACHMENT_BIT
	}
	if u&gpu.TextureDepth != 0 || depth {
		f |= C.VK_IMAGE_USAGE_DEPTH_STENCIL_ATTACHMENT_BIT
	}
	if u&gpu.TextureTransfer != 0 {
		f |= C.VK_IMAGE_USAGE_TRANSFER_SRC_BIT | C.VK_IMAGE_USAGE_TRANSFER_DST_BIT
	}
	return f
}

// CreateTexture creates an image (+ memory + default view) and, when
// TextureSampled/TextureStorage, registers a view into the bindless heap
// (Texture.Index is the heap slot).
func (b *Backend) CreateTexture(d gpu.TextureDescriptor) gpu.Texture {
	layers := d.Layers
	if layers == 0 {
		layers = 1
	}
	mips := d.Mips
	if mips == 0 {
		mips = 1
	}
	isDepth := d.Format == gpu.FormatDepth32F || d.Format == gpu.FormatDepth24Stencil8
	aspect := C.VkImageAspectFlags(C.VK_IMAGE_ASPECT_COLOR_BIT)
	if isDepth {
		aspect = C.VK_IMAGE_ASPECT_DEPTH_BIT
	}

	var img C.VkImage
	var mem C.VkDeviceMemory
	var view C.VkImageView
	r := C.vkbCreateImage(b.device, b.physicalDevice, vkFormat(d.Format),
		C.uint32_t(d.Width), C.uint32_t(d.Height), C.uint32_t(layers), C.uint32_t(mips),
		imageUsage(d.Usage, isDepth), aspect, viewType(d.Kind),
		&img, &mem, &view)
	if r != C.VK_SUCCESS {
		panic(fmt.Sprintf("vulkan: CreateTexture(%dx%d, %q) failed (%d)", d.Width, d.Height, d.Label, int(r)))
	}

	h := b.nextID.Add(1)
	b.textures[h] = &textureEntry{
		img: img, mem: mem, view: view, format: vkFormat(d.Format),
		width: d.Width, height: d.Height, layout: C.VK_IMAGE_LAYOUT_UNDEFINED,
		depth: isDepth, owned: true,
	}

	tex := gpu.Texture{H: gpu.Handle(h)}
	if d.Usage&gpu.TextureSampled != 0 {
		tex.Index = b.sampledNext
		b.sampledNext++
		C.vkbWriteSampledImage(b.device, b.descSet, C.uint32_t(tex.Index), view, sampledLayout(isDepth))
	} else if d.Usage&gpu.TextureStorage != 0 {
		tex.Index = b.storageNext
		b.storageNext++
		C.vkbWriteStorageImage(b.device, b.descSet, C.uint32_t(tex.Index), view)
	}
	return tex
}

// TextureView registers an additional sampled view over a subresource range of an
// existing texture (e.g. a single array layer or mip) into the bindless heap and
// returns a Texture whose Index is the new heap slot. The returned handle owns only
// the view (owned=false), so DestroyTexture frees the view but not the image.
func (b *Backend) TextureView(t gpu.Texture, kind gpu.TextureKind, baseMip, mipCount, baseLayer, layerCount uint32) gpu.Texture {
	src, ok := b.textures[uint64(t.H)]
	if !ok {
		panic("vulkan: TextureView of unknown texture")
	}
	aspect := C.VkImageAspectFlags(C.VK_IMAGE_ASPECT_COLOR_BIT)
	if src.depth {
		aspect = C.VK_IMAGE_ASPECT_DEPTH_BIT
	}
	if mipCount == 0 {
		mipCount = 1
	}
	if layerCount == 0 {
		layerCount = 1
	}
	var view C.VkImageView
	r := C.vkbCreateSubView(b.device, src.img, src.format, aspect, viewType(kind),
		C.uint32_t(baseMip), C.uint32_t(mipCount), C.uint32_t(baseLayer), C.uint32_t(layerCount), &view)
	if r != C.VK_SUCCESS {
		panic(fmt.Sprintf("vulkan: TextureView failed (%d)", int(r)))
	}

	h := b.nextID.Add(1)
	b.textures[h] = &textureEntry{
		view: view, format: src.format, width: src.width, height: src.height,
		layout: C.VK_IMAGE_LAYOUT_UNDEFINED, depth: src.depth, owned: false,
	}
	tex := gpu.Texture{H: gpu.Handle(h), Index: b.sampledNext}
	b.sampledNext++
	C.vkbWriteSampledImage(b.device, b.descSet, C.uint32_t(tex.Index), view, sampledLayout(src.depth))
	return tex
}

// DestroyTexture releases a texture (heap slots are not reclaimed yet).
func (b *Backend) DestroyTexture(t gpu.Texture) {
	e, ok := b.textures[uint64(t.H)]
	if !ok {
		return
	}
	if e.owned {
		C.vkbDestroyImage(b.device, e.img, e.mem, e.view)
	} else {
		C.vkDestroyImageView(b.device, e.view, nil)
	}
	delete(b.textures, uint64(t.H))
}

// destroyAllTextures releases every live texture (shutdown).
func (b *Backend) destroyAllTextures() {
	for k, e := range b.textures {
		if e.owned {
			C.vkbDestroyImage(b.device, e.img, e.mem, e.view)
		} else {
			C.vkDestroyImageView(b.device, e.view, nil)
		}
		delete(b.textures, k)
	}
}
