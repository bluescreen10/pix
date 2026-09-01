package vulkan

/*
#include <vulkan/vulkan.h>
#include <stdint.h>

static VkResult vkbCreateCommandPool(VkDevice dev, uint32_t fam, VkCommandPool* out) {
    VkCommandPoolCreateInfo ci = {0};
    ci.sType = VK_STRUCTURE_TYPE_COMMAND_POOL_CREATE_INFO;
    ci.flags = VK_COMMAND_POOL_CREATE_RESET_COMMAND_BUFFER_BIT;
    ci.queueFamilyIndex = fam;
    return vkCreateCommandPool(dev, &ci, NULL, out);
}

static VkResult vkbAllocCmd(VkDevice dev, VkCommandPool pool, VkCommandBuffer* out) {
    VkCommandBufferAllocateInfo ai = {0};
    ai.sType = VK_STRUCTURE_TYPE_COMMAND_BUFFER_ALLOCATE_INFO;
    ai.commandPool = pool;
    ai.level = VK_COMMAND_BUFFER_LEVEL_PRIMARY;
    ai.commandBufferCount = 1;
    return vkAllocateCommandBuffers(dev, &ai, out);
}

static VkResult vkbBeginCmd(VkCommandBuffer cb) {
    VkCommandBufferBeginInfo bi = {0};
    bi.sType = VK_STRUCTURE_TYPE_COMMAND_BUFFER_BEGIN_INFO;
    bi.flags = VK_COMMAND_BUFFER_USAGE_ONE_TIME_SUBMIT_BIT;
    return vkBeginCommandBuffer(cb, &bi);
}

// Bind the global bindless set at both bind points for the whole recording.
static void vkbBindHeap(VkCommandBuffer cb, VkPipelineLayout layout, VkDescriptorSet set) {
    vkCmdBindDescriptorSets(cb, VK_PIPELINE_BIND_POINT_GRAPHICS, layout, 0, 1, &set, 0, NULL);
    vkCmdBindDescriptorSets(cb, VK_PIPELINE_BIND_POINT_COMPUTE, layout, 0, 1, &set, 0, NULL);
}

static VkResult vkbSubmit(VkDevice dev, VkQueue q, VkCommandBuffer cb, VkFence* outFence) {
    VkFenceCreateInfo fi = {0};
    fi.sType = VK_STRUCTURE_TYPE_FENCE_CREATE_INFO;
    VkResult r = vkCreateFence(dev, &fi, NULL, outFence);
    if (r != VK_SUCCESS) return r;
    VkCommandBufferSubmitInfo cbi = {0};
    cbi.sType = VK_STRUCTURE_TYPE_COMMAND_BUFFER_SUBMIT_INFO;
    cbi.commandBuffer = cb;
    VkSubmitInfo2 si = {0};
    si.sType = VK_STRUCTURE_TYPE_SUBMIT_INFO_2;
    si.commandBufferInfoCount = 1;
    si.pCommandBufferInfos = &cbi;
    return vkQueueSubmit2(q, 1, &si, *outFence);
}

// vkbImageBarrier transitions an image between layouts (sync2).
static void vkbImageBarrier(VkCommandBuffer cb, VkImage img, VkImageAspectFlags aspect,
                            VkImageLayout oldL, VkImageLayout newL,
                            VkPipelineStageFlags2 srcStage, VkAccessFlags2 srcAccess,
                            VkPipelineStageFlags2 dstStage, VkAccessFlags2 dstAccess) {
    VkImageMemoryBarrier2 ib = {0};
    ib.sType = VK_STRUCTURE_TYPE_IMAGE_MEMORY_BARRIER_2;
    ib.srcStageMask = srcStage; ib.srcAccessMask = srcAccess;
    ib.dstStageMask = dstStage; ib.dstAccessMask = dstAccess;
    ib.oldLayout = oldL; ib.newLayout = newL;
    ib.image = img;
    ib.subresourceRange.aspectMask = aspect;
    ib.subresourceRange.levelCount = VK_REMAINING_MIP_LEVELS;
    ib.subresourceRange.layerCount = VK_REMAINING_ARRAY_LAYERS;
    VkDependencyInfo di = {0};
    di.sType = VK_STRUCTURE_TYPE_DEPENDENCY_INFO;
    di.imageMemoryBarrierCount = 1;
    di.pImageMemoryBarriers = &ib;
    vkCmdPipelineBarrier2(cb, &di);
}

// vkbGlobalBarrier is the gpu.Barrier: a queue+stage memory barrier, no resources.
static void vkbGlobalBarrier(VkCommandBuffer cb, VkPipelineStageFlags2 srcStage, VkAccessFlags2 srcAccess,
                             VkPipelineStageFlags2 dstStage, VkAccessFlags2 dstAccess) {
    VkMemoryBarrier2 mb = {0};
    mb.sType = VK_STRUCTURE_TYPE_MEMORY_BARRIER_2;
    mb.srcStageMask = srcStage; mb.srcAccessMask = srcAccess;
    mb.dstStageMask = dstStage; mb.dstAccessMask = dstAccess;
    VkDependencyInfo di = {0};
    di.sType = VK_STRUCTURE_TYPE_DEPENDENCY_INFO;
    di.memoryBarrierCount = 1;
    di.pMemoryBarriers = &mb;
    vkCmdPipelineBarrier2(cb, &di);
}

// vkbBeginRendering starts dynamic rendering with one color attachment and an
// optional depth attachment (covers the common case; multi-RT is a TODO).
static void vkbBeginRendering(VkCommandBuffer cb, uint32_t w, uint32_t h,
                              VkImageView color, int colorClear, float cr, float cg, float cb_, float ca,
                              int hasDepth, VkImageView depth, int depthClear, float dclear) {
    VkRenderingAttachmentInfo ci = {0};
    ci.sType = VK_STRUCTURE_TYPE_RENDERING_ATTACHMENT_INFO;
    ci.imageView = color;
    ci.imageLayout = VK_IMAGE_LAYOUT_COLOR_ATTACHMENT_OPTIMAL;
    ci.loadOp = colorClear ? VK_ATTACHMENT_LOAD_OP_CLEAR : VK_ATTACHMENT_LOAD_OP_LOAD;
    ci.storeOp = VK_ATTACHMENT_STORE_OP_STORE;
    ci.clearValue.color.float32[0] = cr; ci.clearValue.color.float32[1] = cg;
    ci.clearValue.color.float32[2] = cb_; ci.clearValue.color.float32[3] = ca;

    VkRenderingAttachmentInfo di = {0};
    di.sType = VK_STRUCTURE_TYPE_RENDERING_ATTACHMENT_INFO;
    di.imageView = depth;
    di.imageLayout = VK_IMAGE_LAYOUT_DEPTH_ATTACHMENT_OPTIMAL;
    di.loadOp = depthClear ? VK_ATTACHMENT_LOAD_OP_CLEAR : VK_ATTACHMENT_LOAD_OP_LOAD;
    di.storeOp = VK_ATTACHMENT_STORE_OP_STORE;
    di.clearValue.depthStencil.depth = dclear;

    VkRenderingInfo ri = {0};
    ri.sType = VK_STRUCTURE_TYPE_RENDERING_INFO;
    ri.renderArea.extent.width = w; ri.renderArea.extent.height = h;
    ri.layerCount = 1;
    // A depth-only pass (shadow map) passes a null color view: advertise zero color
    // attachments so the count matches a pipeline built with no color formats.
    if (color != VK_NULL_HANDLE) {
        ri.colorAttachmentCount = 1;
        ri.pColorAttachments = &ci;
    }
    if (hasDepth) ri.pDepthAttachment = &di;
    vkCmdBeginRendering(cb, &ri);
}

static void vkbCopyImageToBuffer(VkCommandBuffer cb, VkImage img, VkBuffer buf, uint32_t w, uint32_t h,
                                 uint32_t mip, uint32_t layer, VkImageAspectFlags aspect) {
    VkBufferImageCopy c = {0};
    c.imageSubresource.aspectMask = aspect;
    c.imageSubresource.mipLevel = mip;
    c.imageSubresource.baseArrayLayer = layer;
    c.imageSubresource.layerCount = 1;
    c.imageExtent.width = w; c.imageExtent.height = h; c.imageExtent.depth = 1;
    vkCmdCopyImageToBuffer(cb, img, VK_IMAGE_LAYOUT_TRANSFER_SRC_OPTIMAL, buf, 1, &c);
}

static void vkbCopyBufferToImage(VkCommandBuffer cb, VkBuffer buf, uint64_t srcOffset, VkImage img,
                                 uint32_t w, uint32_t h, uint32_t mip, uint32_t layer, VkImageAspectFlags aspect) {
    VkBufferImageCopy c = {0};
    c.bufferOffset = srcOffset;
    c.imageSubresource.aspectMask = aspect;
    c.imageSubresource.mipLevel = mip;
    c.imageSubresource.baseArrayLayer = layer;
    c.imageSubresource.layerCount = 1;
    c.imageExtent.width = w; c.imageExtent.height = h; c.imageExtent.depth = 1;
    vkCmdCopyBufferToImage(cb, buf, img, VK_IMAGE_LAYOUT_TRANSFER_DST_OPTIMAL, 1, &c);
}

static void vkbPush(VkCommandBuffer cb, VkPipelineLayout layout, uint64_t root) {
    vkCmdPushConstants(cb, layout, VK_SHADER_STAGE_ALL, 0, sizeof(uint64_t), &root);
}
*/
import "C"

import (
	"fmt"

	"github.com/bluescreen10/pix/gpu"
)

func (b *Backend) initCommands() error {
	if r := C.vkbCreateCommandPool(b.device, C.uint32_t(b.queueFamily), &b.cmdPool); r != C.VK_SUCCESS {
		return fmt.Errorf("vulkan: command pool creation failed (%d)", int(r))
	}
	b.fences = map[uint64]fenceEntry{}
	return nil
}

func (b *Backend) destroyCommands() {
	for _, f := range b.fences {
		C.vkDestroyFence(b.device, f.fence, nil)
		C.vkFreeCommandBuffers(b.device, b.cmdPool, 1, &f.cb)
	}
	if b.cmdPool != nil {
		C.vkDestroyCommandPool(b.device, b.cmdPool, nil)
	}
}

type fenceEntry struct {
	cb    C.VkCommandBuffer
	fence C.VkFence
}

// cmdBuffer is the Vulkan CommandBuffer: a primary command buffer that has the
// bindless heap bound and the shared pipeline layout for push constants.
type cmdBuffer struct {
	b  *Backend
	cb C.VkCommandBuffer
}

// Begin allocates a transient command buffer, begins recording, and binds the
// global bindless descriptor set.
func (b *Backend) Begin() gpu.CommandBuffer {
	var cb C.VkCommandBuffer
	if r := C.vkbAllocCmd(b.device, b.cmdPool, &cb); r != C.VK_SUCCESS {
		panic(fmt.Sprintf("vulkan: allocate command buffer failed (%d)", int(r)))
	}
	if r := C.vkbBeginCmd(cb); r != C.VK_SUCCESS {
		panic(fmt.Sprintf("vulkan: begin command buffer failed (%d)", int(r)))
	}
	C.vkbBindHeap(cb, b.pipelineLayout, b.descSet)
	return &cmdBuffer{b: b, cb: cb}
}

// Submit ends recording and submits, returning a fence for the completion.
func (b *Backend) Submit(cmd gpu.CommandBuffer) gpu.Fence {
	c := cmd.(*cmdBuffer)
	C.vkEndCommandBuffer(c.cb)
	var fence C.VkFence
	if r := C.vkbSubmit(b.device, b.queue, c.cb, &fence); r != C.VK_SUCCESS {
		panic(fmt.Sprintf("vulkan: submit failed (%d)", int(r)))
	}
	h := b.nextID.Add(1)
	b.fences[h] = fenceEntry{cb: c.cb, fence: fence}
	return gpu.Fence{H: gpu.Handle(h)}
}

// Wait blocks until the fence signals, then frees the command buffer + fence.
func (b *Backend) Wait(f gpu.Fence) {
	e, ok := b.fences[uint64(f.H)]
	if !ok {
		return
	}
	C.vkWaitForFences(b.device, 1, &e.fence, C.VK_TRUE, C.UINT64_MAX)
	C.vkDestroyFence(b.device, e.fence, nil)
	C.vkFreeCommandBuffers(b.device, b.cmdPool, 1, &e.cb)
	delete(b.fences, uint64(f.H))
}

// WaitIdle blocks until the device is idle.
func (b *Backend) WaitIdle() { C.vkDeviceWaitIdle(b.device) }

// --- CommandBuffer ---

func (b *Backend) tex(t gpu.Texture) *textureEntry { return b.textures[uint64(t.H)] }

func (b *Backend) bufRaw(buf gpu.Buffer) C.VkBuffer { return b.buffers[uint64(buf.H)].buf }

func aspectOf(e *textureEntry) C.VkImageAspectFlags {
	if e.depth {
		return C.VK_IMAGE_ASPECT_DEPTH_BIT
	}
	return C.VK_IMAGE_ASPECT_COLOR_BIT
}

// transition moves an image to newLayout from its tracked current layout.
func (c *cmdBuffer) transition(e *textureEntry, newLayout C.VkImageLayout,
	srcStage C.VkPipelineStageFlags2, srcAccess C.VkAccessFlags2,
	dstStage C.VkPipelineStageFlags2, dstAccess C.VkAccessFlags2) {
	C.vkbImageBarrier(c.cb, e.img, aspectOf(e), e.layout, newLayout, srcStage, srcAccess, dstStage, dstAccess)
	e.layout = newLayout
}

func (c *cmdBuffer) BeginRenderPass(rt gpu.RenderTargets) {
	var w, h uint32
	var colorView C.VkImageView
	var clear [4]float32
	colorClear := C.int(0)
	if len(rt.Color) > 0 {
		e := c.b.tex(rt.Color[0].Texture)
		w, h = e.width, e.height
		colorView = e.view
		clear = rt.Color[0].Clear
		if rt.Color[0].Load == gpu.LoadClear {
			colorClear = 1
		}
		c.transition(e, C.VK_IMAGE_LAYOUT_COLOR_ATTACHMENT_OPTIMAL,
			C.VK_PIPELINE_STAGE_2_TOP_OF_PIPE_BIT, 0,
			C.VK_PIPELINE_STAGE_2_COLOR_ATTACHMENT_OUTPUT_BIT, C.VK_ACCESS_2_COLOR_ATTACHMENT_WRITE_BIT)
	}
	var depthView C.VkImageView
	hasDepth := C.int(0)
	depthClear := C.int(0)
	var dclear float32
	if rt.Depth != nil {
		e := c.b.tex(rt.Depth.Texture)
		w, h = e.width, e.height
		depthView = e.view
		hasDepth = 1
		dclear = rt.Depth.Clear
		if rt.Depth.Load == gpu.LoadClear {
			depthClear = 1
		}
		c.transition(e, C.VK_IMAGE_LAYOUT_DEPTH_ATTACHMENT_OPTIMAL,
			C.VK_PIPELINE_STAGE_2_TOP_OF_PIPE_BIT, 0,
			C.VK_PIPELINE_STAGE_2_EARLY_FRAGMENT_TESTS_BIT|C.VK_PIPELINE_STAGE_2_LATE_FRAGMENT_TESTS_BIT,
			C.VK_ACCESS_2_DEPTH_STENCIL_ATTACHMENT_WRITE_BIT)
	}
	C.vkbBeginRendering(c.cb, C.uint32_t(w), C.uint32_t(h),
		colorView, colorClear, C.float(clear[0]), C.float(clear[1]), C.float(clear[2]), C.float(clear[3]),
		hasDepth, depthView, depthClear, C.float(dclear))
}

func (c *cmdBuffer) EndRenderPass() { C.vkCmdEndRendering(c.cb) }

// SetPipeline binds a pipeline to its own bind point (graphics or compute),
// recorded when the pipeline was created.
func (c *cmdBuffer) SetPipeline(p gpu.Pipeline) {
	e := c.b.pipelines[uint64(p.H)]
	C.vkCmdBindPipeline(c.cb, e.bindPoint, e.pipe)
}

func (c *cmdBuffer) Root(addr uint64) { C.vkbPush(c.cb, c.b.pipelineLayout, C.uint64_t(addr)) }

func (c *cmdBuffer) Viewport(x, y, width, height, minDepth, maxDepth float32) {
	vp := C.VkViewport{x: C.float(x), y: C.float(y), width: C.float(width), height: C.float(height),
		minDepth: C.float(minDepth), maxDepth: C.float(maxDepth)}
	C.vkCmdSetViewport(c.cb, 0, 1, &vp)
}

func (c *cmdBuffer) Scissor(x, y, width, height int32) {
	sc := C.VkRect2D{offset: C.VkOffset2D{x: C.int32_t(x), y: C.int32_t(y)},
		extent: C.VkExtent2D{width: C.uint32_t(width), height: C.uint32_t(height)}}
	C.vkCmdSetScissor(c.cb, 0, 1, &sc)
}

func (c *cmdBuffer) Draw(vertexCount, instanceCount, firstVertex, firstInstance uint32) {
	C.vkCmdDraw(c.cb, C.uint32_t(vertexCount), C.uint32_t(instanceCount), C.uint32_t(firstVertex), C.uint32_t(firstInstance))
}

func (c *cmdBuffer) DrawIndexed(indexBuf gpu.Buffer, indexCount, instanceCount, firstIndex uint32, vertexOffset int32, firstInstance uint32) {
	C.vkCmdBindIndexBuffer(c.cb, c.b.bufRaw(indexBuf), 0, C.VK_INDEX_TYPE_UINT32)
	C.vkCmdDrawIndexed(c.cb, C.uint32_t(indexCount), C.uint32_t(instanceCount), C.uint32_t(firstIndex), C.int32_t(vertexOffset), C.uint32_t(firstInstance))
}

func (c *cmdBuffer) DrawIndexedIndirect(indexBuf, args gpu.Buffer, argsOffset uint64, drawCount, stride uint32) {
	C.vkCmdBindIndexBuffer(c.cb, c.b.bufRaw(indexBuf), 0, C.VK_INDEX_TYPE_UINT32)
	C.vkCmdDrawIndexedIndirect(c.cb, c.b.bufRaw(args), C.VkDeviceSize(argsOffset), C.uint32_t(drawCount), C.uint32_t(stride))
}

func (c *cmdBuffer) Dispatch(x, y, z uint32) {
	C.vkCmdDispatch(c.cb, C.uint32_t(x), C.uint32_t(y), C.uint32_t(z))
}

func (c *cmdBuffer) DispatchIndirect(args gpu.Buffer, offset uint64) {
	C.vkCmdDispatchIndirect(c.cb, c.b.bufRaw(args), C.VkDeviceSize(offset))
}

func (c *cmdBuffer) Barrier(src, dst gpu.Stage, flags gpu.BarrierFlags) {
	ss, sa := stageAccess(src)
	ds, da := stageAccess(dst)
	C.vkbGlobalBarrier(c.cb, ss, sa, ds, da)
}

func (c *cmdBuffer) PrepareSampled(t gpu.Texture, at gpu.Stage) {
	e := c.b.tex(t)
	ds, _ := stageAccess(at)
	if ds == 0 {
		ds = C.VK_PIPELINE_STAGE_2_FRAGMENT_SHADER_BIT
	}
	c.transition(e, C.VK_IMAGE_LAYOUT_SHADER_READ_ONLY_OPTIMAL,
		C.VK_PIPELINE_STAGE_2_COPY_BIT, C.VK_ACCESS_2_TRANSFER_WRITE_BIT,
		ds, C.VK_ACCESS_2_SHADER_READ_BIT)
}

func (c *cmdBuffer) CopyBuffer(dst, src gpu.Buffer, dstOffset, srcOffset, size uint64) {
	region := C.VkBufferCopy{srcOffset: C.VkDeviceSize(srcOffset), dstOffset: C.VkDeviceSize(dstOffset), size: C.VkDeviceSize(size)}
	C.vkCmdCopyBuffer(c.cb, c.b.bufRaw(src), c.b.bufRaw(dst), 1, &region)
}

func (c *cmdBuffer) CopyBufferToTexture(dst gpu.Texture, mip, layer uint32, src gpu.Buffer, srcOffset uint64) {
	e := c.b.tex(dst)
	c.transition(e, C.VK_IMAGE_LAYOUT_TRANSFER_DST_OPTIMAL,
		C.VK_PIPELINE_STAGE_2_TOP_OF_PIPE_BIT, 0,
		C.VK_PIPELINE_STAGE_2_COPY_BIT, C.VK_ACCESS_2_TRANSFER_WRITE_BIT)
	C.vkbCopyBufferToImage(c.cb, c.b.bufRaw(src), C.uint64_t(srcOffset), e.img,
		C.uint32_t(e.width), C.uint32_t(e.height), C.uint32_t(mip), C.uint32_t(layer), aspectOf(e))
}

func (c *cmdBuffer) CopyTextureToBuffer(dst gpu.Buffer, src gpu.Texture, mip, layer uint32) {
	e := c.b.tex(src)
	c.transition(e, C.VK_IMAGE_LAYOUT_TRANSFER_SRC_OPTIMAL,
		C.VK_PIPELINE_STAGE_2_COLOR_ATTACHMENT_OUTPUT_BIT, C.VK_ACCESS_2_COLOR_ATTACHMENT_WRITE_BIT,
		C.VK_PIPELINE_STAGE_2_COPY_BIT, C.VK_ACCESS_2_TRANSFER_READ_BIT)
	C.vkbCopyImageToBuffer(c.cb, e.img, c.b.bufRaw(dst),
		C.uint32_t(e.width), C.uint32_t(e.height), C.uint32_t(mip), C.uint32_t(layer), aspectOf(e))
}

// stageAccess maps an gpu.Stage bitmask to a coarse (stage, access) pair.
func stageAccess(s gpu.Stage) (C.VkPipelineStageFlags2, C.VkAccessFlags2) {
	var stage C.VkPipelineStageFlags2
	var access C.VkAccessFlags2
	if s == gpu.StageNone {
		return C.VK_PIPELINE_STAGE_2_NONE, C.VK_ACCESS_2_NONE
	}
	if s == gpu.StageAll {
		return C.VK_PIPELINE_STAGE_2_ALL_COMMANDS_BIT, C.VK_ACCESS_2_MEMORY_READ_BIT | C.VK_ACCESS_2_MEMORY_WRITE_BIT
	}
	if s&gpu.StageIndirect != 0 {
		stage |= C.VK_PIPELINE_STAGE_2_DRAW_INDIRECT_BIT
		access |= C.VK_ACCESS_2_INDIRECT_COMMAND_READ_BIT
	}
	if s&gpu.StageVertex != 0 {
		stage |= C.VK_PIPELINE_STAGE_2_VERTEX_SHADER_BIT
		access |= C.VK_ACCESS_2_SHADER_READ_BIT
	}
	if s&gpu.StageFragment != 0 {
		stage |= C.VK_PIPELINE_STAGE_2_FRAGMENT_SHADER_BIT
		access |= C.VK_ACCESS_2_SHADER_READ_BIT
	}
	if s&gpu.StageColorOutput != 0 {
		stage |= C.VK_PIPELINE_STAGE_2_COLOR_ATTACHMENT_OUTPUT_BIT
		access |= C.VK_ACCESS_2_COLOR_ATTACHMENT_WRITE_BIT
	}
	if s&gpu.StageDepth != 0 {
		stage |= C.VK_PIPELINE_STAGE_2_EARLY_FRAGMENT_TESTS_BIT | C.VK_PIPELINE_STAGE_2_LATE_FRAGMENT_TESTS_BIT
		access |= C.VK_ACCESS_2_DEPTH_STENCIL_ATTACHMENT_WRITE_BIT
	}
	if s&gpu.StageCompute != 0 {
		stage |= C.VK_PIPELINE_STAGE_2_COMPUTE_SHADER_BIT
		access |= C.VK_ACCESS_2_SHADER_READ_BIT | C.VK_ACCESS_2_SHADER_WRITE_BIT
	}
	if s&gpu.StageTransfer != 0 {
		stage |= C.VK_PIPELINE_STAGE_2_ALL_TRANSFER_BIT
		access |= C.VK_ACCESS_2_TRANSFER_READ_BIT | C.VK_ACCESS_2_TRANSFER_WRITE_BIT
	}
	return stage, access
}
