package vulkan

/*
#include <vulkan/vulkan.h>
#include <stdint.h>
#include <stdlib.h>

static VkResult vkbShaderModule(VkDevice dev, const uint32_t* code, size_t bytes, VkShaderModule* out) {
    VkShaderModuleCreateInfo ci = {0};
    ci.sType = VK_STRUCTURE_TYPE_SHADER_MODULE_CREATE_INFO;
    ci.codeSize = bytes;
    ci.pCode = code;
    return vkCreateShaderModule(dev, &ci, NULL, out);
}

static VkResult vkbCreateComputePipeline(VkDevice dev, VkPipelineLayout layout,
                                         const void* code, size_t bytes, const char* entry, VkPipeline* out) {
    VkShaderModule mod;
    VkResult r = vkbShaderModule(dev, (const uint32_t*)code, bytes, &mod);
    if (r != VK_SUCCESS) return r;

    VkComputePipelineCreateInfo ci = {0};
    ci.sType = VK_STRUCTURE_TYPE_COMPUTE_PIPELINE_CREATE_INFO;
    ci.layout = layout;
    ci.stage.sType = VK_STRUCTURE_TYPE_PIPELINE_SHADER_STAGE_CREATE_INFO;
    ci.stage.stage = VK_SHADER_STAGE_COMPUTE_BIT;
    ci.stage.module = mod;
    ci.stage.pName = entry;

    r = vkCreateComputePipelines(dev, VK_NULL_HANDLE, 1, &ci, NULL, out);
    vkDestroyShaderModule(dev, mod, NULL);
    return r;
}

// vkbCreateGraphicsPipeline builds a minimal graphics pipeline: vs+fs, empty
// vertex input (geometry is pulled via BDA), dynamic viewport/scissor, dynamic
// rendering (formats via VkPipelineRenderingCreateInfo, renderPass = NULL),
// opaque color targets. Depth is enabled iff depthFormat != UNDEFINED.
static VkResult vkbCreateCreateGraphicsPipeline(VkDevice dev, VkPipelineLayout layout,
        const void* vs, size_t vsBytes, const void* fs, size_t fsBytes, const char* entry,
        VkPrimitiveTopology topo, const VkFormat* colorFmts, uint32_t nColor,
        VkFormat depthFmt, VkCullModeFlags cull, int frontFaceCW, int blendMode,
        int depthTest, int depthWrite, VkCompareOp depthCompare, uint32_t samples, VkPipeline* out) {
    VkShaderModule vmod, fmod;
    VkResult r = vkbShaderModule(dev, (const uint32_t*)vs, vsBytes, &vmod);
    if (r != VK_SUCCESS) return r;
    r = vkbShaderModule(dev, (const uint32_t*)fs, fsBytes, &fmod);
    if (r != VK_SUCCESS) { vkDestroyShaderModule(dev, vmod, NULL); return r; }

    VkPipelineShaderStageCreateInfo stages[2] = {0};
    stages[0].sType = VK_STRUCTURE_TYPE_PIPELINE_SHADER_STAGE_CREATE_INFO;
    stages[0].stage = VK_SHADER_STAGE_VERTEX_BIT; stages[0].module = vmod; stages[0].pName = entry;
    stages[1].sType = VK_STRUCTURE_TYPE_PIPELINE_SHADER_STAGE_CREATE_INFO;
    stages[1].stage = VK_SHADER_STAGE_FRAGMENT_BIT; stages[1].module = fmod; stages[1].pName = entry;

    VkPipelineVertexInputStateCreateInfo vin = {0};
    vin.sType = VK_STRUCTURE_TYPE_PIPELINE_VERTEX_INPUT_STATE_CREATE_INFO;

    VkPipelineInputAssemblyStateCreateInfo ia = {0};
    ia.sType = VK_STRUCTURE_TYPE_PIPELINE_INPUT_ASSEMBLY_STATE_CREATE_INFO;
    ia.topology = topo;

    VkPipelineViewportStateCreateInfo vp = {0};
    vp.sType = VK_STRUCTURE_TYPE_PIPELINE_VIEWPORT_STATE_CREATE_INFO;
    vp.viewportCount = 1; vp.scissorCount = 1; // dynamic

    VkPipelineRasterizationStateCreateInfo rs = {0};
    rs.sType = VK_STRUCTURE_TYPE_PIPELINE_RASTERIZATION_STATE_CREATE_INFO;
    rs.polygonMode = VK_POLYGON_MODE_FILL;
    rs.cullMode = cull;
    rs.frontFace = frontFaceCW ? VK_FRONT_FACE_CLOCKWISE : VK_FRONT_FACE_COUNTER_CLOCKWISE;
    rs.lineWidth = 1.0f;

    VkPipelineMultisampleStateCreateInfo ms = {0};
    ms.sType = VK_STRUCTURE_TYPE_PIPELINE_MULTISAMPLE_STATE_CREATE_INFO;
    ms.rasterizationSamples = samples < 2 ? VK_SAMPLE_COUNT_1_BIT : (VkSampleCountFlagBits)samples;

    VkPipelineDepthStencilStateCreateInfo ds = {0};
    ds.sType = VK_STRUCTURE_TYPE_PIPELINE_DEPTH_STENCIL_STATE_CREATE_INFO;
    ds.depthTestEnable = depthTest ? VK_TRUE : VK_FALSE;
    ds.depthWriteEnable = depthWrite ? VK_TRUE : VK_FALSE;
    ds.depthCompareOp = depthCompare;

    // blendMode: 0 = opaque, 1 = src-alpha over, 2 = additive.
    VkPipelineColorBlendAttachmentState atts[8] = {0};
    for (uint32_t i = 0; i < nColor && i < 8; i++) {
        atts[i].colorWriteMask = VK_COLOR_COMPONENT_R_BIT | VK_COLOR_COMPONENT_G_BIT | VK_COLOR_COMPONENT_B_BIT | VK_COLOR_COMPONENT_A_BIT;
        if (blendMode == 0) {
            atts[i].blendEnable = VK_FALSE;
        } else {
            atts[i].blendEnable = VK_TRUE;
            atts[i].colorBlendOp = VK_BLEND_OP_ADD;
            atts[i].alphaBlendOp = VK_BLEND_OP_ADD;
            atts[i].srcAlphaBlendFactor = VK_BLEND_FACTOR_ONE;
            atts[i].dstAlphaBlendFactor = VK_BLEND_FACTOR_ONE_MINUS_SRC_ALPHA;
            if (blendMode == 1) { // src-alpha over
                atts[i].srcColorBlendFactor = VK_BLEND_FACTOR_SRC_ALPHA;
                atts[i].dstColorBlendFactor = VK_BLEND_FACTOR_ONE_MINUS_SRC_ALPHA;
            } else { // additive
                atts[i].srcColorBlendFactor = VK_BLEND_FACTOR_SRC_ALPHA;
                atts[i].dstColorBlendFactor = VK_BLEND_FACTOR_ONE;
            }
        }
    }
    VkPipelineColorBlendStateCreateInfo cb = {0};
    cb.sType = VK_STRUCTURE_TYPE_PIPELINE_COLOR_BLEND_STATE_CREATE_INFO;
    cb.attachmentCount = nColor;
    cb.pAttachments = atts;

    VkDynamicState dyn[2] = { VK_DYNAMIC_STATE_VIEWPORT, VK_DYNAMIC_STATE_SCISSOR };
    VkPipelineDynamicStateCreateInfo dsi = {0};
    dsi.sType = VK_STRUCTURE_TYPE_PIPELINE_DYNAMIC_STATE_CREATE_INFO;
    dsi.dynamicStateCount = 2; dsi.pDynamicStates = dyn;

    VkPipelineRenderingCreateInfo rci = {0};
    rci.sType = VK_STRUCTURE_TYPE_PIPELINE_RENDERING_CREATE_INFO;
    rci.colorAttachmentCount = nColor;
    rci.pColorAttachmentFormats = colorFmts;
    rci.depthAttachmentFormat = depthFmt;

    VkGraphicsPipelineCreateInfo ci = {0};
    ci.sType = VK_STRUCTURE_TYPE_GRAPHICS_PIPELINE_CREATE_INFO;
    ci.pNext = &rci; // dynamic rendering
    ci.stageCount = 2; ci.pStages = stages;
    ci.pVertexInputState = &vin;
    ci.pInputAssemblyState = &ia;
    ci.pViewportState = &vp;
    ci.pRasterizationState = &rs;
    ci.pMultisampleState = &ms;
    ci.pDepthStencilState = &ds;
    ci.pColorBlendState = &cb;
    ci.pDynamicState = &dsi;
    ci.layout = layout;
    ci.renderPass = VK_NULL_HANDLE; // dynamic rendering

    r = vkCreateGraphicsPipelines(dev, VK_NULL_HANDLE, 1, &ci, NULL, out);
    vkDestroyShaderModule(dev, vmod, NULL);
    vkDestroyShaderModule(dev, fmod, NULL);
    return r;
}
*/
import "C"

import (
	"fmt"
	"unsafe"

	"github.com/bluescreen10/pix/gpu"
)

func topology(t gpu.Topology) C.VkPrimitiveTopology {
	switch t {
	case gpu.TopologyTriangleStrip:
		return C.VK_PRIMITIVE_TOPOLOGY_TRIANGLE_STRIP
	case gpu.TopologyLines:
		return C.VK_PRIMITIVE_TOPOLOGY_LINE_LIST
	case gpu.TopologyPoints:
		return C.VK_PRIMITIVE_TOPOLOGY_POINT_LIST
	default:
		return C.VK_PRIMITIVE_TOPOLOGY_TRIANGLE_LIST
	}
}

func cullMode(m gpu.CullMode) C.VkCullModeFlags {
	switch m {
	case gpu.CullBack:
		return C.VK_CULL_MODE_BACK_BIT
	case gpu.CullFront:
		return C.VK_CULL_MODE_FRONT_BIT
	default:
		return C.VK_CULL_MODE_NONE
	}
}

// ComputePipeline compiles a compute pipeline from SPIR-V, sharing the global
// bindless pipeline layout.
func (b *Backend) CreateComputePipeline(desc gpu.ComputePipelineDescriptor) gpu.Pipeline {
	if desc.Entry == "" {
		desc.Entry = "main"
	}
	cEntry := C.CString(desc.Entry)
	defer C.free(unsafe.Pointer(cEntry))
	var p C.VkPipeline
	r := C.vkbCreateComputePipeline(b.device, b.pipelineLayout,
		unsafe.Pointer(&desc.Shader[0]), C.size_t(len(desc.Shader)), cEntry, &p)
	if r != C.VK_SUCCESS {
		panic(fmt.Sprintf("vulkan: CreateComputePipeline(%q) failed (%d)", desc.Label, int(r)))
	}
	return b.registerPipeline(p, C.VK_PIPELINE_BIND_POINT_COMPUTE)
}

// GraphicsPipeline compiles a graphics pipeline (dynamic rendering), sharing the
// global bindless pipeline layout.
func (b *Backend) CreateGraphicsPipeline(d gpu.PipelineDescriptor) gpu.Pipeline {
	entry := d.VertexEntry
	if entry == "" {
		entry = "main"
	}
	cEntry := C.CString(entry)
	defer C.free(unsafe.Pointer(cEntry))

	colorFmts := make([]C.VkFormat, len(d.ColorFormats))
	for i, f := range d.ColorFormats {
		colorFmts[i] = vkFormat(f)
	}
	var colorPtr *C.VkFormat
	if len(colorFmts) > 0 {
		colorPtr = &colorFmts[0]
	}
	depthTest := C.int(0)
	if d.DepthTest {
		depthTest = 1
	}
	depthWrite := C.int(0)
	if d.DepthWrite {
		depthWrite = 1
	}
	samples := d.Samples
	if samples == 0 {
		samples = 1
	}
	// blendMode: 0 opaque, 1 src-alpha over, 2 additive (from the first target's
	// color op destination factor).
	blendMode := C.int(0)
	if len(d.Blend) > 0 && d.Blend[0].Enable {
		if d.Blend[0].ColorOp.Dst == gpu.BlendOne {
			blendMode = 2
		} else {
			blendMode = 1
		}
	}

	frontFaceCW := C.int(0)
	if d.FrontFaceCW {
		frontFaceCW = 1
	}

	var p C.VkPipeline
	r := C.vkbCreateCreateGraphicsPipeline(b.device, b.pipelineLayout,
		unsafe.Pointer(&d.VertexShader[0]), C.size_t(len(d.VertexShader)),
		unsafe.Pointer(&d.FragmentShader[0]), C.size_t(len(d.FragmentShader)), cEntry,
		topology(d.Topology), colorPtr, C.uint32_t(len(d.ColorFormats)),
		vkFormat(d.DepthFormat), cullMode(d.CullMode), frontFaceCW, blendMode,
		depthTest, depthWrite, compareOp(d.DepthCompare), C.uint32_t(samples), &p)
	if r != C.VK_SUCCESS {
		panic(fmt.Sprintf("vulkan: CreateGraphicsPipeline(%q) failed (%d)", d.Label, int(r)))
	}
	return b.registerPipeline(p, C.VK_PIPELINE_BIND_POINT_GRAPHICS)
}

// pipelineEntry records a pipeline and the bind point it was created for, so
// SetPipeline can bind it without the caller distinguishing compute vs graphics.
type pipelineEntry struct {
	pipe      C.VkPipeline
	bindPoint C.VkPipelineBindPoint
}

func (b *Backend) registerPipeline(p C.VkPipeline, bindPoint C.VkPipelineBindPoint) gpu.Pipeline {
	h := b.nextH
	b.nextH++
	b.pipelines[h] = pipelineEntry{pipe: p, bindPoint: bindPoint}
	return gpu.Pipeline{H: gpu.Handle(h)}
}

// DestroyPipeline releases a pipeline.
func (b *Backend) DestroyPipeline(p gpu.Pipeline) {
	e, ok := b.pipelines[uint64(p.H)]
	if !ok {
		return
	}
	C.vkDestroyPipeline(b.device, e.pipe, nil)
	delete(b.pipelines, uint64(p.H))
}
