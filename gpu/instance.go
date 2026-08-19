// Package gpu is the engine's GPU device/resource layer. It owns all direct
// contact with the underlying graphics API (currently WebGPU via dawn-go) so
// the rest of the engine depends on this package's handles rather than wgpu
// types. It is deliberately not a general RHI yet — the surface is only what
// the renderer needs, shaped around WebGPU, with clean seams for a future
// native backend.
package gpu

import (
	"fmt"
	"os"

	"github.com/bluescreen10/dawn-go/wgpu"
	"github.com/bluescreen10/pix/internal/mem"
)

// Instance owns the device, queue, surface and their configuration. All fields
// are private; callers go through the creation methods below (and, during the
// migration, the transitional Device/Queue/Surface accessors).
type Instance struct {
	adapter  *wgpu.Adapter
	config   wgpu.SurfaceConfiguration
	device   *wgpu.Device
	format   wgpu.TextureFormat
	queue    *wgpu.Queue
	surface  *wgpu.Surface
	features map[wgpu.FeatureName]bool

	// buffers is the registry behind Buffer handles.
	buffers mem.Slab[bufferData]
}

var forceFallbackAdapter = os.Getenv("WGPU_FORCE_FALLBACK_ADAPTER") == "1"

// Init creates the instance, surface, adapter, device and queue, and configures
// the surface for the given size.
func (i *Instance) Init(width, height uint32, descriptor wgpu.SurfaceDescriptor) error {
	i.buffers = mem.NewSlab[bufferData]()

	instance := wgpu.CreateInstance(nil)
	defer instance.Release()

	i.surface = instance.CreateSurface(descriptor)

	adapter, err := instance.RequestAdapter(&wgpu.RequestAdapterOptions{
		ForceFallbackAdapter: forceFallbackAdapter,
		CompatibleSurface:    i.surface,
	})
	if err != nil {
		return err
	}
	i.adapter = adapter

	i.features = make(map[wgpu.FeatureName]bool)
	for _, f := range adapter.GetFeatures() {
		i.features[f] = true
	}

	var requiredFeatures []wgpu.FeatureName
	if i.features[wgpu.FeatureNameTimestampQuery] {
		requiredFeatures = append(requiredFeatures, wgpu.FeatureNameTimestampQuery)
	}

	i.device = i.adapter.RequestDevice(&wgpu.DeviceDescriptor{
		RequiredFeatures: requiredFeatures,
		UncapturedErrorCallback: wgpu.UncapturedErrorCallback(func(device *wgpu.Device, typ wgpu.ErrorType, message string) {
			panic(fmt.Sprintf("(%s): %s", typ, message))
		}),
	})
	i.queue = i.device.GetQueue()

	caps, err := i.surface.GetCapabilities(i.adapter)
	if err != nil {
		return err
	}
	i.format = caps.Formats[0]

	i.config = wgpu.SurfaceConfiguration{
		Usage:       wgpu.TextureUsageRenderAttachment,
		Format:      i.format,
		Width:       width,
		Height:      height,
		PresentMode: wgpu.PresentModeFifo,
		AlphaMode:   caps.AlphaModes[0],
		Device:      i.device,
	}
	i.surface.Configure(i.config)

	return nil
}

// Destroy releases the device, queue, surface and adapter.
func (i *Instance) Destroy() {
	i.surface.Release()
	i.queue.Release()
	i.device.Release()
	i.adapter.Release()

	i.surface = nil
	i.queue = nil
	i.device = nil
	i.adapter = nil
	i.config = wgpu.SurfaceConfiguration{}
	i.features = make(map[wgpu.FeatureName]bool)

	// Destroy dedicated buffers. Sub-allocations share their arena's backing
	// buffer, which is destroyed once per arena below — destroying b.raw here
	// would double-free it.
	for b := range i.buffers.Items() {
		b.raw.Destroy()
	}
}

// --- Resource creation ------------------------------------------------------
// These return raw wgpu handles for now; the return types are narrowed to gpu
// package types as more of the renderer moves in.

func (i *Instance) CreateShaderModule(desc wgpu.ShaderModuleDescriptor) *wgpu.ShaderModule {
	return i.device.CreateShaderModule(desc)
}

func (i *Instance) CreateRenderPipeline(desc wgpu.RenderPipelineDescriptor) *wgpu.RenderPipeline {
	return i.device.CreateRenderPipeline(desc)
}

func (i *Instance) CreateComputePipeline(desc wgpu.ComputePipelineDescriptor) *wgpu.ComputePipeline {
	return i.device.CreateComputePipeline(desc)
}

func (i *Instance) CreatePipelineLayout(desc wgpu.PipelineLayoutDescriptor) *wgpu.PipelineLayout {
	return i.device.CreatePipelineLayout(desc)
}

func (i *Instance) CreateBindGroup(desc wgpu.BindGroupDescriptor) *wgpu.BindGroup {
	return i.device.CreateBindGroup(desc)
}

func (i *Instance) CreateBindGroupLayout(desc wgpu.BindGroupLayoutDescriptor) *wgpu.BindGroupLayout {
	return i.device.CreateBindGroupLayout(desc)
}

func (i *Instance) CreateSampler(desc *wgpu.SamplerDescriptor) *wgpu.Sampler {
	return i.device.CreateSampler(desc)
}

func (i *Instance) CreateTexture(desc *wgpu.TextureDescriptor) *wgpu.Texture {
	return i.device.CreateTexture(desc)
}

func (i *Instance) CreateCommandEncoder(desc *wgpu.CommandEncoderDescriptor) *wgpu.CommandEncoder {
	return i.device.CreateCommandEncoder(desc)
}

// --- Transitional accessors -------------------------------------------------
// These expose the raw device/queue/surface so call sites that haven't moved to
// the typed API yet keep working. They'll be removed as those sites migrate.

func (i *Instance) Device() *wgpu.Device       { return i.device }
func (i *Instance) Queue() *wgpu.Queue         { return i.queue }
func (i *Instance) Surface() *wgpu.Surface     { return i.surface }
func (i *Instance) Format() wgpu.TextureFormat { return i.format }
