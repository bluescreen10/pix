package vulkan

/*
#include <vulkan/vulkan.h>
#include <stdint.h>

// Bindless heap binding slots (set 0). One global set, shared by every pipeline.
enum {
    BIND_SAMPLED = 0, // texture2D[]  (sampled images)
    BIND_STORAGE = 1, // image2D[]    (storage images)
    BIND_SAMPLER = 2, // sampler[]
};

static uint32_t vkbMin3(uint32_t a, uint32_t b, uint32_t c) {
    uint32_t m = a < b ? a : b;
    return m < c ? m : c;
}

// vkbCreateBindlessHeap builds the one descriptor set layout (3 update-after-bind
// arrays), pool, persistent set, and the pipeline layout every pipeline shares
// (that set + a push-constant holding the 64-bit root pointer). Capacities are
// clamped to the device's update-after-bind limits; the clamped values are
// returned so the Go side can bound its slot allocators.
static VkResult vkbCreateBindlessHeap(VkDevice dev, VkPhysicalDevice phys,
                                      uint32_t wantSampled, uint32_t wantStorage, uint32_t wantSampler,
                                      VkDescriptorSetLayout* outLayout, VkDescriptorPool* outPool,
                                      VkDescriptorSet* outSet, VkPipelineLayout* outPipeLayout,
                                      uint32_t* capSampled, uint32_t* capStorage, uint32_t* capSampler) {
    VkPhysicalDeviceVulkan12Properties p12 = {0};
    p12.sType = VK_STRUCTURE_TYPE_PHYSICAL_DEVICE_VULKAN_1_2_PROPERTIES;
    VkPhysicalDeviceProperties2 p2 = {0};
    p2.sType = VK_STRUCTURE_TYPE_PHYSICAL_DEVICE_PROPERTIES_2;
    p2.pNext = &p12;
    vkGetPhysicalDeviceProperties2(phys, &p2);

    uint32_t nSampled = vkbMin3(wantSampled, p12.maxPerStageDescriptorUpdateAfterBindSampledImages, p12.maxDescriptorSetUpdateAfterBindSampledImages);
    uint32_t nStorage = vkbMin3(wantStorage, p12.maxPerStageDescriptorUpdateAfterBindStorageImages, p12.maxDescriptorSetUpdateAfterBindStorageImages);
    uint32_t nSampler = vkbMin3(wantSampler, p12.maxPerStageDescriptorUpdateAfterBindSamplers, p12.maxDescriptorSetUpdateAfterBindSamplers);
    *capSampled = nSampled;
    *capStorage = nStorage;
    *capSampler = nSampler;

    VkDescriptorSetLayoutBinding binds[3] = {0};
    binds[0].binding = BIND_SAMPLED; binds[0].descriptorType = VK_DESCRIPTOR_TYPE_SAMPLED_IMAGE; binds[0].descriptorCount = nSampled; binds[0].stageFlags = VK_SHADER_STAGE_ALL;
    binds[1].binding = BIND_STORAGE; binds[1].descriptorType = VK_DESCRIPTOR_TYPE_STORAGE_IMAGE; binds[1].descriptorCount = nStorage; binds[1].stageFlags = VK_SHADER_STAGE_ALL;
    binds[2].binding = BIND_SAMPLER; binds[2].descriptorType = VK_DESCRIPTOR_TYPE_SAMPLER;       binds[2].descriptorCount = nSampler; binds[2].stageFlags = VK_SHADER_STAGE_ALL;

    VkDescriptorBindingFlags flags[3];
    for (int i = 0; i < 3; i++) flags[i] = VK_DESCRIPTOR_BINDING_UPDATE_AFTER_BIND_BIT | VK_DESCRIPTOR_BINDING_PARTIALLY_BOUND_BIT;
    VkDescriptorSetLayoutBindingFlagsCreateInfo bf = {0};
    bf.sType = VK_STRUCTURE_TYPE_DESCRIPTOR_SET_LAYOUT_BINDING_FLAGS_CREATE_INFO;
    bf.bindingCount = 3;
    bf.pBindingFlags = flags;

    VkDescriptorSetLayoutCreateInfo lci = {0};
    lci.sType = VK_STRUCTURE_TYPE_DESCRIPTOR_SET_LAYOUT_CREATE_INFO;
    lci.pNext = &bf;
    lci.flags = VK_DESCRIPTOR_SET_LAYOUT_CREATE_UPDATE_AFTER_BIND_POOL_BIT;
    lci.bindingCount = 3;
    lci.pBindings = binds;

    VkResult r = vkCreateDescriptorSetLayout(dev, &lci, NULL, outLayout);
    if (r != VK_SUCCESS) {
        return r;
    }

    VkDescriptorPoolSize sizes[3];
    sizes[0].type = VK_DESCRIPTOR_TYPE_SAMPLED_IMAGE; sizes[0].descriptorCount = nSampled;
    sizes[1].type = VK_DESCRIPTOR_TYPE_STORAGE_IMAGE; sizes[1].descriptorCount = nStorage;
    sizes[2].type = VK_DESCRIPTOR_TYPE_SAMPLER;       sizes[2].descriptorCount = nSampler;
    VkDescriptorPoolCreateInfo pci = {0};
    pci.sType = VK_STRUCTURE_TYPE_DESCRIPTOR_POOL_CREATE_INFO;
    pci.flags = VK_DESCRIPTOR_POOL_CREATE_UPDATE_AFTER_BIND_BIT;
    pci.maxSets = 1;
    pci.poolSizeCount = 3;
    pci.pPoolSizes = sizes;

    r = vkCreateDescriptorPool(dev, &pci, NULL, outPool);
    if (r != VK_SUCCESS) {
        vkDestroyDescriptorSetLayout(dev, *outLayout, NULL);
        return r;
    }

    VkDescriptorSetAllocateInfo sai = {0};
    sai.sType = VK_STRUCTURE_TYPE_DESCRIPTOR_SET_ALLOCATE_INFO;
    sai.descriptorPool = *outPool;
    sai.descriptorSetCount = 1;
    sai.pSetLayouts = outLayout;

    r = vkAllocateDescriptorSets(dev, &sai, outSet);
    if (r != VK_SUCCESS) {
        vkDestroyDescriptorPool(dev, *outPool, NULL);
        vkDestroyDescriptorSetLayout(dev, *outLayout, NULL);
        return r;
    }

    // Every pipeline shares this layout: the global set + a 128-byte push
    // constant. Root() writes the 64-bit root pointer into the first 8 bytes.
    VkPushConstantRange pcr = {0};
    pcr.stageFlags = VK_SHADER_STAGE_ALL;
    pcr.offset = 0;
    pcr.size = 128;
    VkPipelineLayoutCreateInfo plci = {0};
    plci.sType = VK_STRUCTURE_TYPE_PIPELINE_LAYOUT_CREATE_INFO;
    plci.setLayoutCount = 1;
    plci.pSetLayouts = outLayout;
    plci.pushConstantRangeCount = 1;
    plci.pPushConstantRanges = &pcr;


    r = vkCreatePipelineLayout(dev, &plci, NULL, outPipeLayout);
    if (r != VK_SUCCESS) {
        vkDestroyDescriptorPool(dev, *outPool, NULL); // frees the allocated set too
        vkDestroyDescriptorSetLayout(dev, *outLayout, NULL);
        return r;
    }
    return VK_SUCCESS;
}

static VkResult vkbCreateSampler(VkDevice dev, VkFilter mag, VkFilter min, VkSamplerMipmapMode mip,
                                 VkSamplerAddressMode u, VkSamplerAddressMode v, VkSamplerAddressMode w,
                                 int compareEnable, VkCompareOp compareOp, float maxAniso, VkSampler* out) {
    VkSamplerCreateInfo ci = {0};
    ci.sType = VK_STRUCTURE_TYPE_SAMPLER_CREATE_INFO;
    ci.magFilter = mag; ci.minFilter = min; ci.mipmapMode = mip;
    ci.addressModeU = u; ci.addressModeV = v; ci.addressModeW = w;
    ci.minLod = 0.0f; ci.maxLod = VK_LOD_CLAMP_NONE;
    ci.compareEnable = compareEnable ? VK_TRUE : VK_FALSE;
    ci.compareOp = compareOp;
    ci.anisotropyEnable = maxAniso > 1.0f ? VK_TRUE : VK_FALSE;
    ci.maxAnisotropy = maxAniso < 1.0f ? 1.0f : maxAniso;
    return vkCreateSampler(dev, &ci, NULL, out);
}

static void vkbWriteSampler(VkDevice dev, VkDescriptorSet set, uint32_t index, VkSampler s) {
    VkDescriptorImageInfo ii = {0};
    ii.sampler = s;
    VkWriteDescriptorSet w = {0};
    w.sType = VK_STRUCTURE_TYPE_WRITE_DESCRIPTOR_SET;
    w.dstSet = set;
    w.dstBinding = BIND_SAMPLER;
    w.dstArrayElement = index;
    w.descriptorCount = 1;
    w.descriptorType = VK_DESCRIPTOR_TYPE_SAMPLER;
    w.pImageInfo = &ii;
    vkUpdateDescriptorSets(dev, 1, &w, 0, NULL);
}
*/
import "C"

import (
	"fmt"

	"github.com/bluescreen10/pix/gpu"
)

// Desired heap capacities (clamped to device update-after-bind limits at init).
const (
	wantSampledImages = 16384
	wantStorageImages = 4096
	wantSamplers      = 512
)

// initBindless creates the global descriptor heap + shared pipeline layout.
// TODO: make wantedSampledImages, wantStorageImages, and wantSamplers configurable
func (b *Backend) initBindless() error {
	var cs, cst, csa C.uint32_t
	r := C.vkbCreateBindlessHeap(b.device, b.physicalDevice,
		wantSampledImages, wantStorageImages, wantSamplers,
		&b.setLayout, &b.descPool, &b.descSet, &b.pipelineLayout,
		&cs, &cst, &csa)
	if r != C.VK_SUCCESS {
		return fmt.Errorf("vulkan: bindless heap creation failed (%d)", int(r))
	}
	b.capSampled, b.capStorage, b.capSampler = uint32(cs), uint32(cst), uint32(csa)
	b.samplers = map[uint64]C.VkSampler{}
	return nil
}

func (b *Backend) destroyBindless() {
	for _, s := range b.samplers {
		C.vkDestroySampler(b.device, s, nil)
	}
	if b.pipelineLayout != nil {
		C.vkDestroyPipelineLayout(b.device, b.pipelineLayout, nil)
	}
	if b.descPool != nil {
		C.vkDestroyDescriptorPool(b.device, b.descPool, nil) // frees the set too
	}
	if b.setLayout != nil {
		C.vkDestroyDescriptorSetLayout(b.device, b.setLayout, nil)
	}
}

func filter(linear bool) C.VkFilter {
	if linear {
		return C.VK_FILTER_LINEAR
	}
	return C.VK_FILTER_NEAREST
}

func mipmapMode(linear bool) C.VkSamplerMipmapMode {
	if linear {
		return C.VK_SAMPLER_MIPMAP_MODE_LINEAR
	}
	return C.VK_SAMPLER_MIPMAP_MODE_NEAREST
}

func addressMode(m gpu.AddressMode) C.VkSamplerAddressMode {
	switch m {
	case gpu.AddressRepeat:
		return C.VK_SAMPLER_ADDRESS_MODE_REPEAT
	case gpu.AddressMirror:
		return C.VK_SAMPLER_ADDRESS_MODE_MIRRORED_REPEAT
	default:
		return C.VK_SAMPLER_ADDRESS_MODE_CLAMP_TO_EDGE
	}
}

// CreateSampler registers a sampler into the bindless heap and returns its index.
func (b *Backend) CreateSampler(d gpu.SamplerDescriptor) gpu.Sampler {
	compareEnable := C.int(0)
	if d.Compare != gpu.CompareNever {
		compareEnable = 1
	}
	var s C.VkSampler
	r := C.vkbCreateSampler(b.device,
		filter(d.MagLinear), filter(d.MinLinear), mipmapMode(d.MipLinear),
		addressMode(d.AddressU), addressMode(d.AddressV), addressMode(d.AddressW),
		compareEnable, compareOp(d.Compare), C.float(d.MaxAnisotropy), &s)
	if r != C.VK_SUCCESS {
		panic(fmt.Sprintf("vulkan: CreateSampler failed (%d)", int(r)))
	}
	index := b.samplerNext
	b.samplerNext++
	C.vkbWriteSampler(b.device, b.descSet, C.uint32_t(index), s)

	h := b.nextID.Add(1)
	b.samplers[h] = s
	return gpu.Sampler{Index: index, H: gpu.Handle(h)}
}

// DestroySampler releases a sampler (its heap slot is not reclaimed yet).
func (b *Backend) DestroySampler(s gpu.Sampler) {
	vs, ok := b.samplers[uint64(s.H)]
	if !ok {
		return
	}
	C.vkDestroySampler(b.device, vs, nil)
	delete(b.samplers, uint64(s.H))
}

// compareOp maps an gpu compare op to Vulkan.
func compareOp(op gpu.CompareOp) C.VkCompareOp {
	switch op {
	case gpu.CompareLess:
		return C.VK_COMPARE_OP_LESS
	case gpu.CompareEqual:
		return C.VK_COMPARE_OP_EQUAL
	case gpu.CompareLessEqual:
		return C.VK_COMPARE_OP_LESS_OR_EQUAL
	case gpu.CompareGreater:
		return C.VK_COMPARE_OP_GREATER
	case gpu.CompareNotEqual:
		return C.VK_COMPARE_OP_NOT_EQUAL
	case gpu.CompareGreaterEqual:
		return C.VK_COMPARE_OP_GREATER_OR_EQUAL
	case gpu.CompareAlways:
		return C.VK_COMPARE_OP_ALWAYS
	default:
		return C.VK_COMPARE_OP_NEVER
	}
}

// HeapCapacities returns the clamped bindless array sizes (diagnostics).
func (b *Backend) HeapCapacities() (sampled, storage, sampler uint32) {
	return b.capSampled, b.capStorage, b.capSampler
}
