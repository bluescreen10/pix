// Package gpu is pix's rendering hardware interface: a thin, bindless,
// GPU-driven abstraction over a modern explicit API. It is modelled on
// Sebastian Aaltonen's "no graphics API" proposal and maps directly onto
// Vulkan 1.4 (the backend targets apiVersion 1.4; the features it relies on —
// buffer_device_address, descriptor_indexing, dynamic_rendering,
// synchronization2 — are core since 1.3, so no extension juggling on 1.4).
// The first Backend is Vulkan (via KosmicKrisp on macOS); native Metal / D3D12
// backends can implement the same interface later.
//
// The design is deliberately minimal:
//
//   - Memory is a flat allocator (Alloc) returning buffers that expose a 64-bit
//     device address (BDA) and, for host memory, a persistently-mapped pointer.
//     Shaders read/write data through raw pointers, not descriptor sets.
//   - Textures live in one global bindless heap; a Texture is a 32-bit index a
//     shader uses to sample. There are no per-draw texture bindings.
//   - All per-draw/dispatch data flows through a single 64-bit "root" pointer
//     (a push constant holding the address of a struct in mapped memory).
//   - There are no render passes (dynamic rendering), no input layouts / vertex
//     buffers (shaders load geometry from buffers), no resource-layout
//     transitions, and barriers are queue+stage granularity with no resource
//     lists.
package gpu

import "unsafe"

// ----------------------------------------------------------------------------
// Handles
// ----------------------------------------------------------------------------

// Handle is a backend-defined opaque id (e.g. a slab index into the backend's
// resource registry). It is exported so a backend in another package can set it,
// but it is otherwise private to that backend — callers must not interpret it.
type Handle uint64

// Buffer is a GPU allocation. Addr is its device address (BDA): pass it to
// shaders directly or embed it in a root struct. Ptr is a persistently-mapped
// CPU pointer for MemoryHost allocations (nil for MemoryDevice); write straight
// through it — no staging, no descriptor updates. H is the backend handle.
type Buffer struct {
	Addr uint64
	Ptr  unsafe.Pointer //TODO: turn into uintptr
	Size uint64
	H    Handle
}

// Valid reports whether the buffer refers to a live allocation.
func (b Buffer) Valid() bool { return b.H != 0 }

// Texture is a slot in the global bindless heap. Index is what a shader uses to
// sample (nonuniformEXT(index) into the sampled-image array). H is the backend
// handle (for render-target/storage/destroy use).
type Texture struct {
	Index uint32
	H     Handle
}

func (t Texture) Valid() bool { return t.H != 0 }

// Sampler is a slot in the bindless sampler heap; Index is used from shaders.
type Sampler struct {
	Index uint32
	H     Handle
}

// Pipeline is a compiled compute or graphics pipeline. Graphics pipelines carry
// only formats + topology + minimal state; everything else is dynamic or root data.
type Pipeline struct{ H Handle }

func (p Pipeline) Valid() bool { return p.H != 0 }

// Swapchain is a window's presentation chain. Backbuffers are surfaced as
// Textures (render targets) via AcquireNext.
type Swapchain struct{ H Handle }

// Fence is a submission completion token.
type Fence struct{ H Handle }

// QueryPool is a set of GPU timestamp queries (for profiling pass/frame time).
type QueryPool struct{ H Handle }

// Valid reports whether the pool was created.
func (q QueryPool) Valid() bool { return q.H != 0 }

// ----------------------------------------------------------------------------
// Enums
// ----------------------------------------------------------------------------

// MemoryType selects where an allocation lives and whether it is CPU-mapped.
type MemoryType uint8

const (
	// MemoryHost is CPU-visible, coherent, persistently mapped (ReBAR-style):
	// the buffer exposes both Addr and Ptr. Ideal for per-frame upload data.
	MemoryHost MemoryType = iota
	// MemoryDevice is GPU-only: Addr but no Ptr. Populate via CopyBuffer.
	MemoryDevice
)

// Format is a texel/attachment format (subset; extend as needed).
type Format uint16

const (
	FormatUndefined Format = iota
	FormatR8Unorm
	FormatRG8Unorm
	FormatRGBA8Unorm
	FormatRGBA8Srgb
	FormatBGRA8Unorm
	FormatBGRA8Srgb
	FormatRG16F
	FormatRGBA16F
	FormatR32F
	FormatRGBA32F
	FormatRGB10A2Unorm
	FormatDepth32F
	FormatDepth24Stencil8
)

// TextureUsage is a bitmask of intended uses.
type TextureUsage uint16

const (
	TextureSampled      TextureUsage = 1 << iota // sampled in shaders (bindless heap)
	TextureStorage                               // read/write storage image
	TextureRenderTarget                          // color attachment
	TextureDepth                                 // depth/stencil attachment
	TextureTransfer                              // copy src/dst
)

// TextureKind selects the view dimension.
type TextureKind uint8

const (
	Texture2D TextureKind = iota
	Texture2DArray
	TextureCube
	TextureCubeArray
	Texture3D
)

// Stage is a coarse pipeline stage for barriers (synchronization2). Combine with OR.
type Stage uint32

const (
	StageNone        Stage = 0
	StageIndirect    Stage = 1 << iota // indirect command consumption
	StageVertex                        // vertex shading + attribute/index fetch
	StageFragment                      // fragment shading
	StageColorOutput                   // color attachment writes
	StageDepth                         // depth/stencil test+write
	StageCompute                       // compute shading
	StageTransfer                      // copies/blits
	StageAll         Stage = 0xFFFFFFFF
)

// BarrierFlags carry the rare extra hazards that stage granularity can't express.
type BarrierFlags uint32

const (
	// HazardDescriptors: descriptor-heap contents written this batch must be
	// visible to subsequent shader access (Aaltonen's only hazard flag).
	HazardDescriptors BarrierFlags = 1 << iota
)

// Topology is the primitive assembly mode.
type Topology uint8

const (
	TopologyTriangles Topology = iota
	TopologyTriangleStrip
	TopologyLines
	TopologyPoints
)

// CullMode selects face culling.
type CullMode uint8

const (
	CullNone CullMode = iota
	CullBack
	CullFront
)

// CompareOp is a depth/stencil comparison.
type CompareOp uint8

const (
	CompareNever CompareOp = iota
	CompareLess
	CompareEqual
	CompareLessEqual
	CompareGreater
	CompareNotEqual
	CompareGreaterEqual
	CompareAlways
)

// LoadOp / StoreOp control attachment load/store in dynamic rendering.
type LoadOp uint8

const (
	LoadDontCare LoadOp = iota
	LoadClear
	LoadKeep
)

type StoreOp uint8

const (
	StoreDontCare StoreOp = iota
	StoreKeep
)

// ----------------------------------------------------------------------------
// Descriptors
// ----------------------------------------------------------------------------

// TextureDescriptor describes a texture to create. It is registered into the bindless
// heap when it has TextureSampled usage; the returned Texture.Index is the heap slot.
type TextureDescriptor struct {
	Kind          TextureKind
	Width, Height uint32
	Depth         uint32 // 3D depth; else 1
	Layers        uint32 // array layers / cube faces; else 1
	Mips          uint32 // 0 => 1
	Format        Format
	Usage         TextureUsage
	Samples       uint8 // MSAA; 0/1 => 1
	Label         string
}

// SamplerDescriptor describes a bindless sampler.
type SamplerDescriptor struct {
	MinLinear, MagLinear, MipLinear bool
	AddressU, AddressV, AddressW    AddressMode
	Compare                         CompareOp // non-CompareNever => comparison sampler (shadows)
	MaxAnisotropy                   uint8
	Label                           string
}

type AddressMode uint8

const (
	AddressClamp AddressMode = iota
	AddressRepeat
	AddressMirror
)

// BlendState is optional per-target blending (kept minimal, separate from PSO
// permutations per the design).
type BlendState struct {
	Enable                      bool
	SrcColor, DstColor, ColorOp BlendFactorOp
	SrcAlpha, DstAlpha, AlphaOp BlendFactorOp
	WriteMask                   uint8 // bit0..3 = RGBA; 0 => all
}

type BlendFactorOp struct {
	Src, Dst BlendFactor
	Op       BlendOp
}

type BlendFactor uint8

const (
	BlendZero BlendFactor = iota
	BlendOne
	BlendSrcAlpha
	BlendOneMinusSrcAlpha
	BlendDstAlpha
	BlendOneMinusDstAlpha
)

type BlendOp uint8

const (
	BlendAdd BlendOp = iota
	BlendSubtract
	BlendReverseSubtract
	BlendMin
	BlendMax
)

// PipelineDescriptor is the minimal graphics pipeline description: shaders + the
// formats/topology that actually affect codegen. Viewport, scissor, blend
// constants and stencil ref are dynamic; per-draw data comes via the root pointer.
// TODO: Do we standarize in SPIRV? or per platform format e.g. (apple -> mtllib, linux -> spirv )
type PipelineDescriptor struct {
	VertexShader   []byte
	FragmentShader []byte
	VertexEntry    string // "" => "main"
	FragmentEntry  string // "" => "main"

	Topology     Topology
	ColorFormats []Format // dynamic-rendering color attachment formats
	DepthFormat  Format   // FormatUndefined => no depth
	Samples      uint8

	CullMode     CullMode
	FrontFaceCW  bool // front face is clockwise (vs the default counter-clockwise)
	DepthTest    bool
	DepthWrite   bool
	DepthCompare CompareOp
	Blend        []BlendState // per color target; nil => opaque

	Label string
}

type ComputePipelineDescriptor struct {
	Shader []byte
	Entry  string // "" -> "main"
	Label  string
}

// RenderTargets is the dynamic-rendering attachment set for BeginRenderPass.
type RenderTargets struct {
	Color []ColorAttachment
	Depth *DepthAttachment
}

type ColorAttachment struct {
	Texture Texture
	Load    LoadOp
	Store   StoreOp
	Clear   [4]float32
}

type DepthAttachment struct {
	Texture Texture
	Load    LoadOp
	Store   StoreOp
	Clear   float32
	// ReadOnly binds the depth buffer for testing only (no writes), in a layout that
	// also permits sampling the same image from the bindless heap during the pass.
	// A pipeline used with it must have DepthWrite false.
	ReadOnly bool
}

// ----------------------------------------------------------------------------
// Backend
// ----------------------------------------------------------------------------

// Backend is a device implementation (Vulkan first; Metal/D3D12 later). All
// creation is immediate; there are no descriptor sets or pipeline layouts to
// manage — resources are reached through addresses and the bindless heap.
type Backend interface {
	Init() error // This obtains the device, setup queues, etc.
	// Memory.
	Alloc(size uint64, mem MemoryType, label string) Buffer
	Free(Buffer)

	// Bindless resources.
	CreateTexture(TextureDescriptor) Texture
	DestroyTexture(Texture)
	// TextureView registers an additional sampled view (e.g. a single array
	// layer / mip) into the heap and returns its index.
	TextureView(t Texture, kind TextureKind, baseMip, mipCount, baseLayer, layerCount uint32) Texture
	CreateSampler(SamplerDescriptor) Sampler
	DestroySampler(Sampler)

	// Pipelines.
	CreateComputePipeline(ComputePipelineDescriptor) Pipeline
	CreateGraphicsPipeline(PipelineDescriptor) Pipeline
	DestroyPipeline(Pipeline)

	// Presentation. surface is a platform window handle (e.g. CAMetalLayer /
	// HWND / xcb) the backend wraps in a VkSurface. It is an opaque uintptr so the
	// interface stays free of unsafe; backends reinterpret it as needed.
	CreateSwapchain(surface uintptr, width, height uint32) Swapchain
	ResizeSwapchain(sc Swapchain, width, height uint32)
	// AcquireNext returns the next backbuffer as a render-target Texture plus a
	// fence that signals when it's safe to reuse.
	AcquireNext(sc Swapchain) (Texture, Fence)
	// Present ends+submits the recorded command list (synced to the acquire) and
	// presents the backbuffer. Use this instead of Submit for on-screen frames.
	Present(sc Swapchain, cmd CommandBuffer)
	SwapchainFormat(sc Swapchain) Format

	// Command recording (transient per frame).
	Begin() CommandBuffer
	Submit(cl CommandBuffer) Fence

	// Synchronization.
	Wait(Fence)
	WaitIdle()

	// GPU timing. CreateTimestampPool makes a pool of count timestamp slots;
	// ReadTimestamps reads them back (call after the writes have completed, e.g.
	// after Present/WaitIdle). Convert a tick delta to nanoseconds with
	// TimestampPeriod. Record writes with CommandBuffer.WriteTimestamp.
	CreateTimestampPool(count uint32) QueryPool
	DestroyTimestampPool(QueryPool)
	ReadTimestamps(pool QueryPool, count uint32) []uint64
	TimestampPeriod() float64 // nanoseconds per timestamp tick

	Destroy()
}

// CommandBuffer records GPU work. It is transient: acquire with Backend.Begin,
// submit once with Backend.Submit, discard. Recording order is execution order
// on the queue; use Barrier to order dependent stages.
type CommandBuffer interface {
	// Dynamic rendering (no render pass objects).
	BeginRenderPass(RenderTargets)
	EndRenderPass()

	// SetPipeline binds a pipeline; compute vs graphics is inferred from the
	// pipeline's own kind (no separate bind-point call).
	SetPipeline(Pipeline)

	// Root sets the single 64-bit root pointer (push constant) that shaders
	// dereference for all their data. Call after Bind, before draw/dispatch.
	Root(addr uint64)

	// Dynamic state.
	Viewport(x, y, width, height, minDepth, maxDepth float32)
	Scissor(x, y, width, height int32)

	// Draws. No vertex/index buffer bindings — shaders load geometry from
	// buffers via the root pointer / BDA. Index buffer, when used, is an address.
	Draw(vertexCount, instanceCount, firstVertex, firstInstance uint32)
	DrawIndexed(indexBuf Buffer, indexCount, instanceCount, firstIndex uint32, vertexOffset int32, firstInstance uint32)
	DrawIndexedIndirect(indexBuf, args Buffer, argsOffset uint64, drawCount, stride uint32)

	// Compute.
	Dispatch(x, y, z uint32)
	DispatchIndirect(args Buffer, offset uint64)

	// Barrier orders producer→consumer stages on the queue (no resource lists).
	Barrier(src, dst Stage, flags BarrierFlags)

	// GPU timestamps. ResetTimestamps clears a pool's slots (record before writing,
	// outside a rendering scope); WriteTimestamp records the GPU clock into slot
	// index once execution reaches `at`.
	ResetTimestamps(pool QueryPool, count uint32)
	WriteTimestamp(pool QueryPool, index uint32, at Stage)

	// PrepareSampled transitions a texture to be sampled from the bindless heap in
	// shaders (e.g. after an upload copy), making the write visible at stage `at`.
	// The gpu hides layouts, so this is how a producer hands a texture to samplers.
	PrepareSampled(t Texture, at Stage)

	// Copies.
	CopyBuffer(dst, src Buffer, dstOffset, srcOffset, size uint64)
	CopyBufferToTexture(dst Texture, mip, layer uint32, src Buffer, srcOffset uint64)
	CopyTextureToBuffer(dst Buffer, src Texture, mip, layer uint32) // readback/screenshot
}
