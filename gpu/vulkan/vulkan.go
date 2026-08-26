// Package vulkan is the Vulkan 1.4 backend for pix's gpu. It uses thin, targeted
// cgo bindings (not a full Vulkan binding) — only the entry points the bindless,
// GPU-driven gpu needs. On macOS it runs on the KosmicKrisp ICD.
package vulkan

/*
#cgo CFLAGS: -I/opt/homebrew/include
#cgo LDFLAGS: -L/opt/homebrew/lib -lvulkan
#include <vulkan/vulkan.h>
#include <stdlib.h>
#include <string.h>

// createInstance targets Vulkan 1.4. Optional instance extensions (e.g. surface
// extensions from the windowing lib) are enabled when nExt > 0.
static VkResult vkbCreateInstance(uint32_t nExt, const char* const* exts, VkInstance* out) {
    VkApplicationInfo app = {0};
    app.sType = VK_STRUCTURE_TYPE_APPLICATION_INFO;
    app.pApplicationName = "pix";
    app.pEngineName = "pix";
    app.apiVersion = VK_API_VERSION_1_4;

    VkInstanceCreateInfo ci = {0};
    ci.sType = VK_STRUCTURE_TYPE_INSTANCE_CREATE_INFO;
    ci.pApplicationInfo = &app;
    ci.enabledExtensionCount = nExt;
    ci.ppEnabledExtensionNames = exts;
    return vkCreateInstance(&ci, NULL, out);
}

// pickPhysical chooses a device, preferring a discrete GPU.
static VkResult vkbPickPhysical(VkInstance inst, VkPhysicalDevice* out) {
    uint32_t n = 0;
    vkEnumeratePhysicalDevices(inst, &n, NULL);
    if (n == 0) return VK_ERROR_INITIALIZATION_FAILED;
    VkPhysicalDevice* devs = (VkPhysicalDevice*)malloc(n * sizeof(VkPhysicalDevice));
    vkEnumeratePhysicalDevices(inst, &n, devs);
    VkPhysicalDevice chosen = devs[0];
    for (uint32_t i = 0; i < n; i++) {
        VkPhysicalDeviceProperties p;
        vkGetPhysicalDeviceProperties(devs[i], &p);
        if (p.deviceType == VK_PHYSICAL_DEVICE_TYPE_DISCRETE_GPU) { chosen = devs[i]; break; }
    }
    *out = chosen;
    free(devs);
    return VK_SUCCESS;
}

// graphicsQueueFamily returns a family index with graphics+compute, or 0xFFFFFFFF.
static uint32_t vkbGraphicsQueueFamily(VkPhysicalDevice pd) {
    uint32_t n = 0;
    vkGetPhysicalDeviceQueueFamilyProperties(pd, &n, NULL);
    VkQueueFamilyProperties* q = (VkQueueFamilyProperties*)malloc(n * sizeof(VkQueueFamilyProperties));
    vkGetPhysicalDeviceQueueFamilyProperties(pd, &n, q);
    uint32_t fam = 0xFFFFFFFF;
    for (uint32_t i = 0; i < n; i++) {
        if ((q[i].queueFlags & VK_QUEUE_GRAPHICS_BIT) && (q[i].queueFlags & VK_QUEUE_COMPUTE_BIT)) { fam = i; break; }
    }
    free(q);
    return fam;
}

// createDevice enables the 1.4 feature chain the gpu relies on: buffer device
// address, descriptor indexing (bindless heaps), dynamic rendering, sync2.
static VkResult vkbCreateDevice(VkPhysicalDevice pd, uint32_t fam, VkDevice* outDev, VkQueue* outQueue) {
    float prio = 1.0f;
    VkDeviceQueueCreateInfo qi = {0};
    qi.sType = VK_STRUCTURE_TYPE_DEVICE_QUEUE_CREATE_INFO;
    qi.queueFamilyIndex = fam;
    qi.queueCount = 1;
    qi.pQueuePriorities = &prio;

    VkPhysicalDeviceVulkan12Features f12 = {0};
    f12.sType = VK_STRUCTURE_TYPE_PHYSICAL_DEVICE_VULKAN_1_2_FEATURES;
    f12.bufferDeviceAddress = VK_TRUE;
    f12.descriptorIndexing = VK_TRUE;
    f12.runtimeDescriptorArray = VK_TRUE;
    f12.descriptorBindingPartiallyBound = VK_TRUE;
    f12.descriptorBindingVariableDescriptorCount = VK_TRUE;
    f12.descriptorBindingSampledImageUpdateAfterBind = VK_TRUE;
    f12.descriptorBindingStorageImageUpdateAfterBind = VK_TRUE;
    f12.descriptorBindingUpdateUnusedWhilePending = VK_TRUE;
    f12.shaderSampledImageArrayNonUniformIndexing = VK_TRUE;
    f12.shaderStorageImageArrayNonUniformIndexing = VK_TRUE;
    f12.scalarBlockLayout = VK_TRUE; // GL_EXT_scalar_block_layout in the GLSL
    f12.timelineSemaphore = VK_TRUE;

    VkPhysicalDeviceVulkan13Features f13 = {0};
    f13.sType = VK_STRUCTURE_TYPE_PHYSICAL_DEVICE_VULKAN_1_3_FEATURES;
    f13.dynamicRendering = VK_TRUE;
    f13.synchronization2 = VK_TRUE;
    f12.pNext = &f13;

    const char* devExts[] = { VK_KHR_SWAPCHAIN_EXTENSION_NAME };

    VkDeviceCreateInfo ci = {0};
    ci.sType = VK_STRUCTURE_TYPE_DEVICE_CREATE_INFO;
    ci.pNext = &f12;
    ci.queueCreateInfoCount = 1;
    ci.pQueueCreateInfos = &qi;
    ci.enabledExtensionCount = 1;
    ci.ppEnabledExtensionNames = devExts;

    VkResult r = vkCreateDevice(pd, &ci, NULL, outDev);
    if (r != VK_SUCCESS) return r;
    vkGetDeviceQueue(*outDev, fam, 0, outQueue);
    return VK_SUCCESS;
}

static void vkbDeviceName(VkPhysicalDevice pd, char* out) {
    VkPhysicalDeviceProperties p;
    vkGetPhysicalDeviceProperties(pd, &p);
    memcpy(out, p.deviceName, VK_MAX_PHYSICAL_DEVICE_NAME_SIZE);
}

static uint32_t vkbApiVersion(VkPhysicalDevice pd) {
    VkPhysicalDeviceProperties p;
    vkGetPhysicalDeviceProperties(pd, &p);
    return p.apiVersion;
}
*/
import "C"

import (
	"fmt"
	"unsafe"

	"github.com/bluescreen10/pix/gpu"
)

func init() {
	// Register a lazily-constructed instance; the renderer calls Init() to actually
	// create the device. Default surface extensions are enabled so a window can be
	// attached later (see defaultInstanceExtensions, which is platform-specific).
	gpu.RegisterBackend(New(defaultInstanceExtensions()...), "vulkan", 2)
}

// Backend implements gpu.Backend (asserted here so signature drift is a compile
// error, not a late failure at the first cross-package use).
var _ gpu.Backend = (*Backend)(nil)

// Backend is the Vulkan implementation of gpu.Backend. Only device init is wired
// so far; memory/bindless/pipelines/swapchain/commands land in sibling files.
type Backend struct {
	instance    C.VkInstance
	phys        C.VkPhysicalDevice
	device      C.VkDevice
	queue       C.VkQueue
	queueFamily uint32

	// Bindless heap: one descriptor set (sampled/storage images + samplers) and
	// the pipeline layout every pipeline shares (that set + push-constant root).
	setLayout      C.VkDescriptorSetLayout
	descPool       C.VkDescriptorPool
	descSet        C.VkDescriptorSet
	pipelineLayout C.VkPipelineLayout

	capSampled, capStorage, capSampler    uint32
	sampledNext, storageNext, samplerNext uint32

	// Transient command recording.
	cmdPool C.VkCommandPool
	fences  map[uint64]fenceEntry

	// Resource registries, keyed by gpu.Handle (backend-private).
	buffers    map[uint64]bufferEntry
	textures   map[uint64]*textureEntry
	samplers   map[uint64]C.VkSampler
	pipelines  map[uint64]pipelineEntry
	swapchains map[uint64]*swapchainState
	queryPools map[uint64]C.VkQueryPool
	activeSwap *swapchainState // set between AcquireNext and Present
	nextH      uint64

	instanceExts []string
}

// New creates the Vulkan instance/device on the first suitable (preferably
// discrete) GPU, enabling the bindless + dynamic-rendering feature set.
// instanceExtensions are extra instance extensions to enable (e.g. the surface
// extensions a windowing lib requires); pass none for headless use.
// New constructs a lazy Vulkan backend: no device is created until Init(). The
// instance extensions (e.g. surface extensions) are stored for Init to enable.
func New(instanceExtensions ...string) *Backend {
	return &Backend{
		buffers:      map[uint64]bufferEntry{},
		textures:     map[uint64]*textureEntry{},
		pipelines:    map[uint64]pipelineEntry{},
		swapchains:   map[uint64]*swapchainState{},
		nextH:        1,
		instanceExts: instanceExtensions,
	}
}

// Init creates the Vulkan instance, device, bindless heap and command pool. Safe to
// call once; a second call is a no-op.
func (b *Backend) Init() error {
	if b.device != nil {
		return nil
	}
	instanceExtensions := b.instanceExts

	cExts := make([]*C.char, len(instanceExtensions))
	for i, e := range instanceExtensions {
		cExts[i] = C.CString(e)
	}
	defer func() {
		for _, s := range cExts {
			C.free(unsafe.Pointer(s))
		}
	}()
	var extPtr **C.char
	if len(cExts) > 0 {
		extPtr = &cExts[0]
	}
	if r := C.vkbCreateInstance(C.uint32_t(len(cExts)), extPtr, &b.instance); r != C.VK_SUCCESS {
		return fmt.Errorf("vulkan: vkCreateInstance failed (%d)", int(r))
	}
	if r := C.vkbPickPhysical(b.instance, &b.phys); r != C.VK_SUCCESS {
		C.vkDestroyInstance(b.instance, nil)
		return fmt.Errorf("vulkan: no physical device (%d)", int(r))
	}
	fam := C.vkbGraphicsQueueFamily(b.phys)
	if uint32(fam) == 0xFFFFFFFF {
		C.vkDestroyInstance(b.instance, nil)
		return fmt.Errorf("vulkan: no graphics+compute queue family")
	}
	b.queueFamily = uint32(fam)
	if r := C.vkbCreateDevice(b.phys, fam, &b.device, &b.queue); r != C.VK_SUCCESS {
		C.vkDestroyInstance(b.instance, nil)
		return fmt.Errorf("vulkan: vkCreateDevice failed (%d) — a required 1.4 feature may be unsupported", int(r))
	}
	if err := b.initBindless(); err != nil {
		C.vkDestroyDevice(b.device, nil)
		C.vkDestroyInstance(b.instance, nil)
		return err
	}
	if err := b.initCommands(); err != nil {
		b.destroyBindless()
		C.vkDestroyDevice(b.device, nil)
		C.vkDestroyInstance(b.instance, nil)
		return err
	}
	return nil
}

// Destroy releases the device and instance.
func (b *Backend) Destroy() {
	if b.device != nil {
		C.vkDeviceWaitIdle(b.device)
		for _, p := range b.pipelines {
			C.vkDestroyPipeline(b.device, p.pipe, nil)
		}
		for _, qp := range b.queryPools {
			C.vkDestroyQueryPool(b.device, qp, nil)
		}
		b.destroyAllSwapchains()
		b.destroyAllTextures()
		b.destroyAllBuffers()
		b.destroyCommands()
		b.destroyBindless()
		C.vkDestroyDevice(b.device, nil)
		b.device = nil
	}
	if b.instance != nil {
		C.vkDestroyInstance(b.instance, nil)
		b.instance = nil
	}
}

// InstanceHandle returns the VkInstance as a uintptr, for a windowing library to
// create a surface (e.g. glfw Window.CreateWindowSurface).
func (b *Backend) InstanceHandle() uintptr { return uintptr(unsafe.Pointer(b.instance)) }

// DeviceName returns the selected GPU's name (diagnostics).
func (b *Backend) DeviceName() string {
	var buf [C.VK_MAX_PHYSICAL_DEVICE_NAME_SIZE]C.char
	C.vkbDeviceName(b.phys, &buf[0])
	return C.GoString(&buf[0])
}

// APIVersion returns the device's supported Vulkan version as (major, minor, patch).
func (b *Backend) APIVersion() (uint32, uint32, uint32) {
	v := uint32(C.vkbApiVersion(b.phys))
	return (v >> 22) & 0x7F, (v >> 12) & 0x3FF, v & 0xFFF
}
