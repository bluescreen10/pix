package vulkan

/*
#include <vulkan/vulkan.h>
#include <stdlib.h>

static VkQueryPool vkbCreateTimestampPool(VkDevice dev, uint32_t count) {
    VkQueryPoolCreateInfo ci = {0};
    ci.sType = VK_STRUCTURE_TYPE_QUERY_POOL_CREATE_INFO;
    ci.queryType = VK_QUERY_TYPE_TIMESTAMP;
    ci.queryCount = count;
    VkQueryPool pool = VK_NULL_HANDLE;
    vkCreateQueryPool(dev, &ci, NULL, &pool);
    return pool;
}

// vkbGetTimestamps reads count 64-bit results into out. Returns VK_SUCCESS when all
// are available. WITH the WAIT bit it blocks until ready.
static VkResult vkbGetTimestamps(VkDevice dev, VkQueryPool pool, uint32_t count, uint64_t* out) {
    return vkGetQueryPoolResults(dev, pool, 0, count, count * sizeof(uint64_t), out,
        sizeof(uint64_t), VK_QUERY_RESULT_64_BIT | VK_QUERY_RESULT_WAIT_BIT);
}

static float vkbTimestampPeriod(VkPhysicalDevice pd) {
    VkPhysicalDeviceProperties p;
    vkGetPhysicalDeviceProperties(pd, &p);
    return p.limits.timestampPeriod; // nanoseconds per tick
}

static uint32_t vkbTimestampValidBits(VkPhysicalDevice pd, uint32_t fam) {
    uint32_t n = 0;
    vkGetPhysicalDeviceQueueFamilyProperties(pd, &n, NULL);
    VkQueueFamilyProperties* q = (VkQueueFamilyProperties*)malloc(n * sizeof(VkQueueFamilyProperties));
    vkGetPhysicalDeviceQueueFamilyProperties(pd, &n, q);
    uint32_t bits = (fam < n) ? q[fam].timestampValidBits : 0;
    free(q);
    return bits;
}

static void vkbCmdResetQueryPool(VkCommandBuffer cb, VkQueryPool pool, uint32_t count) {
    vkCmdResetQueryPool(cb, pool, 0, count);
}

static void vkbCmdWriteTimestamp(VkCommandBuffer cb, VkPipelineStageFlags2 stage, VkQueryPool pool, uint32_t index) {
    vkCmdWriteTimestamp2(cb, stage, pool, index);
}
*/
import "C"

import "github.com/bluescreen10/pix/gpu"

// CreateTimestampPool allocates a pool of count timestamp query slots.
func (b *Backend) CreateTimestampPool(count uint32) gpu.QueryPool {
	pool := C.vkbCreateTimestampPool(b.device, C.uint32_t(count))
	h := b.nextH
	b.nextH++
	if b.queryPools == nil {
		b.queryPools = map[uint64]C.VkQueryPool{}
	}
	b.queryPools[h] = pool
	return gpu.QueryPool{H: gpu.Handle(h)}
}

// DestroyTimestampPool releases a timestamp pool.
func (b *Backend) DestroyTimestampPool(p gpu.QueryPool) {
	if pool, ok := b.queryPools[uint64(p.H)]; ok {
		C.vkDestroyQueryPool(b.device, pool, nil)
		delete(b.queryPools, uint64(p.H))
	}
}

// ReadTimestamps blocks until the pool's count results are available and returns
// them as raw ticks (convert deltas with TimestampPeriod). Returns nil if the pool
// has no valid results yet.
func (b *Backend) ReadTimestamps(p gpu.QueryPool, count uint32) []uint64 {
	pool, ok := b.queryPools[uint64(p.H)]
	if !ok {
		return nil
	}
	out := make([]uint64, count)
	if r := C.vkbGetTimestamps(b.device, pool, C.uint32_t(count), (*C.uint64_t)(&out[0])); r != C.VK_SUCCESS {
		return nil
	}
	return out
}

// TimestampPeriod returns nanoseconds per timestamp tick for this device.
func (b *Backend) TimestampPeriod() float64 {
	return float64(C.vkbTimestampPeriod(b.phys))
}

// TimestampValidBits returns how many bits of the graphics/compute queue's
// timestamps are meaningful (0 = timestamps unsupported on that queue).
func (b *Backend) TimestampValidBits() uint32 {
	return uint32(C.vkbTimestampValidBits(b.phys, C.uint32_t(b.queueFamily)))
}

func (c *cmdList) ResetTimestamps(p gpu.QueryPool, count uint32) {
	if pool, ok := c.b.queryPools[uint64(p.H)]; ok {
		C.vkbCmdResetQueryPool(c.cb, pool, C.uint32_t(count))
	}
}

func (c *cmdList) WriteTimestamp(p gpu.QueryPool, index uint32, at gpu.Stage) {
	pool, ok := c.b.queryPools[uint64(p.H)]
	if !ok {
		return
	}
	stage, _ := stageAccess(at)
	if stage == 0 {
		// StageNone → the earliest point, for a frame-start timestamp.
		stage = C.VK_PIPELINE_STAGE_2_TOP_OF_PIPE_BIT
	}
	C.vkbCmdWriteTimestamp(c.cb, stage, pool, C.uint32_t(index))
}
