package vulkan

/*
#include <vulkan/vulkan.h>
#include <stdint.h>

// vkbAllocBuffer creates a buffer + backing memory (one dedicated allocation),
// binds them, maps host-visible memory, and returns the device address. Buffers
// carry every usage the bindless model needs, so a single Alloc serves as
// storage / index / indirect / transfer / uniform and exposes a BDA.
static VkResult vkbAllocBuffer(VkDevice dev, VkPhysicalDevice phys, VkDeviceSize size, int hostVisible,
                               VkBuffer* outBuf, VkDeviceMemory* outMem, void** outPtr, VkDeviceAddress* outAddr) {
    VkBufferCreateInfo bi = {0};
    bi.sType = VK_STRUCTURE_TYPE_BUFFER_CREATE_INFO;
    bi.size = size;
    bi.usage = VK_BUFFER_USAGE_STORAGE_BUFFER_BIT | VK_BUFFER_USAGE_UNIFORM_BUFFER_BIT |
               VK_BUFFER_USAGE_INDEX_BUFFER_BIT | VK_BUFFER_USAGE_INDIRECT_BUFFER_BIT |
               VK_BUFFER_USAGE_TRANSFER_SRC_BIT | VK_BUFFER_USAGE_TRANSFER_DST_BIT |
               VK_BUFFER_USAGE_SHADER_DEVICE_ADDRESS_BIT;
    bi.sharingMode = VK_SHARING_MODE_EXCLUSIVE;

    VkResult r = vkCreateBuffer(dev, &bi, NULL, outBuf);
    if (r != VK_SUCCESS) {
        return r;
    }

    VkMemoryRequirements req;
    vkGetBufferMemoryRequirements(dev, *outBuf, &req);

    VkPhysicalDeviceMemoryProperties mp;
    vkGetPhysicalDeviceMemoryProperties(phys, &mp);
    VkMemoryPropertyFlags want = hostVisible
        ? (VK_MEMORY_PROPERTY_HOST_VISIBLE_BIT | VK_MEMORY_PROPERTY_HOST_COHERENT_BIT)
        : VK_MEMORY_PROPERTY_DEVICE_LOCAL_BIT;
    uint32_t typeIndex = 0xFFFFFFFF;
    for (uint32_t i = 0; i < mp.memoryTypeCount; i++) {
        if ((req.memoryTypeBits & (1u << i)) && (mp.memoryTypes[i].propertyFlags & want) == want) {
            typeIndex = i;
            break;
        }
    }
    if (typeIndex == 0xFFFFFFFF) {
        vkDestroyBuffer(dev, *outBuf, NULL);
        return VK_ERROR_OUT_OF_DEVICE_MEMORY;
    }

    VkMemoryAllocateFlagsInfo fi = {0};
    fi.sType = VK_STRUCTURE_TYPE_MEMORY_ALLOCATE_FLAGS_INFO;
    fi.flags = VK_MEMORY_ALLOCATE_DEVICE_ADDRESS_BIT;

    VkMemoryAllocateInfo ai = {0};
    ai.sType = VK_STRUCTURE_TYPE_MEMORY_ALLOCATE_INFO;
    ai.pNext = &fi;
    ai.allocationSize = req.size;
    ai.memoryTypeIndex = typeIndex;

    r = vkAllocateMemory(dev, &ai, NULL, outMem);
    if (r != VK_SUCCESS) {
        vkDestroyBuffer(dev, *outBuf, NULL);
        return r;
    }

    r = vkBindBufferMemory(dev, *outBuf, *outMem, 0);
    if (r != VK_SUCCESS) {
        vkFreeMemory(dev, *outMem, NULL); vkDestroyBuffer(dev, *outBuf, NULL);
        return r;
    }

    if (hostVisible) {
        r = vkMapMemory(dev, *outMem, 0, VK_WHOLE_SIZE, 0, outPtr);
        if (r != VK_SUCCESS) {
            vkFreeMemory(dev, *outMem, NULL); vkDestroyBuffer(dev, *outBuf, NULL);
            return r;
        }
    } else {
        *outPtr = NULL;
    }

    VkBufferDeviceAddressInfo dai = {0};
    dai.sType = VK_STRUCTURE_TYPE_BUFFER_DEVICE_ADDRESS_INFO;
    dai.buffer = *outBuf;
    *outAddr = vkGetBufferDeviceAddress(dev, &dai);
    return VK_SUCCESS;
}

static void vkbFreeBuffer(VkDevice dev, VkBuffer buf, VkDeviceMemory mem) {
    vkDestroyBuffer(dev, buf, NULL);
    vkFreeMemory(dev, mem, NULL);
}
*/
import "C"

import (
	"fmt"
	"unsafe"

	"github.com/bluescreen10/pix/gpu"
)

// bufferEntry is the backend-side record for a Buffer handle.
type bufferEntry struct {
	buf C.VkBuffer
	mem C.VkDeviceMemory
}

// Alloc creates a dedicated buffer + memory. MemoryHost is CPU-mapped and
// coherent (Buffer.Ptr set); MemoryDevice is GPU-only (Ptr nil). Both expose a
// device address (Buffer.Addr). The engine suballocates within an Alloc via
// offsets on Addr — the gpu itself does no suballocation.
func (b *Backend) Alloc(size uint64, mem gpu.MemoryType, label string) gpu.Buffer {
	var (
		buf  C.VkBuffer
		dmem C.VkDeviceMemory
		ptr  unsafe.Pointer
		addr C.VkDeviceAddress
	)
	host := C.int(0)
	if mem == gpu.MemoryHost {
		host = 1
	}
	r := C.vkbAllocBuffer(b.device, b.physicalDevice, C.VkDeviceSize(size), host, &buf, &dmem, &ptr, &addr)
	if r != C.VK_SUCCESS {
		panic(fmt.Sprintf("vulkan: Alloc(%d bytes, %q) failed (%d)", size, label, int(r)))
	}

	h := b.nextID.Add(1)
	b.buffers[h] = bufferEntry{buf: buf, mem: dmem}
	return gpu.Buffer{Addr: uint64(addr), Ptr: ptr, Size: size, H: gpu.Handle(h)}
}

// Free destroys the buffer and releases its memory. No-op on a stale handle.
func (b *Backend) Free(buf gpu.Buffer) {
	e, ok := b.buffers[uint64(buf.H)]
	if !ok {
		return
	}
	C.vkbFreeBuffer(b.device, e.buf, e.mem)
	delete(b.buffers, uint64(buf.H))
}

// destroyAllBuffers releases every live buffer (shutdown).
func (b *Backend) destroyAllBuffers() {
	for k, e := range b.buffers {
		C.vkbFreeBuffer(b.device, e.buf, e.mem)
		delete(b.buffers, k)
	}
}
