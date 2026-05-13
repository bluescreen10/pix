package pix

import (
	"log/slog"
	"math/bits"
	"os"
	"sync"
	"time"
	"unsafe"

	"github.com/bluescreen10/dawn-go/wgpu"
	"github.com/bluescreen10/pix/glm"
	"github.com/bluescreen10/wesl-go"
)

const (
	InitialStorageCapacity = 1024
	MaxDirectionalLights   = 5
	DefaultShadowMapSize   = 1024
)

// Shader Sets
const (
	GlobalSet = iota
	MaterialSet
	InstanceSet
)

// Global Bindings
const (
	CameraBinding = iota
	LightsBinding
	ShadowMapBinding
	ShadowSamplerBinding
)

// Instance Bindings
const (
	InstancesBinding = iota
)

var instancesPool = sync.Pool{
	New: func() any {
		return make(InstancesUniform, 0, InitialStorageCapacity)
	},
}

var renderListPool = sync.Pool{
	New: func() any {
		return &renderList{}
	},
}

var drawingsPool = sync.Pool{
	New: func() any {
		return make([]drawing, 0, 4096)
	},
}

type renderContext struct {
	texture *wgpu.Texture
	view    *wgpu.TextureView
	encoder *wgpu.CommandEncoder
}

type Renderer struct {
	geometries slab[GeometryData]
	materials  slab[MaterialData]
	textures   slab[TextureData]

	samplerCache  map[Sampler]*wgpu.Sampler
	defaultTexRef Ref[Texture]
	deferredFree  []deferredFreeEntry

	runtime       *wgpuRuntime
	width, height uint32
	frameCount    uint32
	logger        *slog.Logger

	pipelineCache *pipelineCache

	// global
	cameraUniformBuffer   *wgpu.Buffer
	lightsUniformBuffer   *wgpu.Buffer
	globalBindGroupLayout *wgpu.BindGroupLayout
	globalBindGroup       *wgpu.BindGroup

	// instance
	instanceStorageBuffer          *wgpu.Buffer
	instanceStorageBindGroupLayout *wgpu.BindGroupLayout
	instanceStorageBindGroup       *wgpu.BindGroup
	instanceStorageCapacity        uint32

	// shadow
	shadowMapTexture    *wgpu.Texture
	shadowMapView       *wgpu.TextureView
	shadowMapLayerViews [MaxDirectionalLights]*wgpu.TextureView
	shadowSampler       *wgpu.Sampler
	shadowCamBuffer     *wgpu.Buffer
	shadowCamBGL        *wgpu.BindGroupLayout
	shadowCamBG         *wgpu.BindGroup
	shadowPipeline      *wgpu.RenderPipeline

	// depth buffer
	depthTexture     *wgpu.Texture
	depthTextureView *wgpu.TextureView

	Stats   *RendererStats
	shaders *wesl.Compiler
}

func NewRenderer(width, height uint32) *Renderer {
	return &Renderer{
		width:         width,
		height:        height,
		logger:        slog.New(slog.NewTextHandler(os.Stderr, nil)),
		runtime:       &wgpuRuntime{},
		Stats:         NewRendererStats(60),
		pipelineCache: newPipelineCache(),
		shaders:       wesl.New(),
	}
}

func (r *Renderer) Init(descriptor wgpu.SurfaceDescriptor) error {
	if err := r.runtime.init(r.width, r.height, descriptor); err != nil {
		slog.Error("error creating runtime", slog.Any("err", err))
		return err
	}

	r.initResources()
	r.shaders.ParseFS(shaderlib)
	r.createGlobalResources()
	return nil
}

func (r *Renderer) Destroy() {
	r.runtime.Destroy()
	r.runtime = nil

	if r.instanceStorageBindGroup != nil {
		r.instanceStorageBindGroup.Release()
		r.instanceStorageBindGroup = nil
	}

	r.cameraUniformBuffer.Destroy()
	r.cameraUniformBuffer = nil

	r.lightsUniformBuffer.Destroy()
	r.lightsUniformBuffer = nil

	if r.instanceStorageBuffer != nil {
		r.instanceStorageBuffer.Destroy()
		r.instanceStorageBuffer = nil
	}

	r.globalBindGroupLayout.Release()
	r.globalBindGroupLayout = nil

	if r.instanceStorageBindGroupLayout != nil {
		r.instanceStorageBindGroupLayout.Release()
		r.instanceStorageBindGroupLayout = nil
	}

	for i := range r.shadowMapLayerViews {
		if r.shadowMapLayerViews[i] != nil {
			r.shadowMapLayerViews[i].Release()
			r.shadowMapLayerViews[i] = nil
		}
	}
	if r.shadowMapView != nil {
		r.shadowMapView.Release()
		r.shadowMapView = nil
	}
	if r.shadowMapTexture != nil {
		r.shadowMapTexture.Destroy()
		r.shadowMapTexture = nil
	}
	if r.shadowSampler != nil {
		r.shadowSampler.Release()
		r.shadowSampler = nil
	}
	if r.shadowCamBuffer != nil {
		r.shadowCamBuffer.Destroy()
		r.shadowCamBuffer = nil
	}
	if r.shadowCamBGL != nil {
		r.shadowCamBGL.Release()
		r.shadowCamBGL = nil
	}
	if r.shadowCamBG != nil {
		r.shadowCamBG.Release()
		r.shadowCamBG = nil
	}
	if r.shadowPipeline != nil {
		r.shadowPipeline.Release()
		r.shadowPipeline = nil
	}

	if r.depthTextureView != nil {
		r.depthTextureView.Release()
		r.depthTexture = nil
	}

	if r.depthTexture != nil {
		r.depthTexture.Destroy()
		r.depthTexture = nil
	}

	r.destroyResources()
}

func (r *Renderer) ensureInstanceStorageSize(needInstances uint32) {
	if r.instanceStorageBuffer == nil || r.instanceStorageCapacity < needInstances {
		if r.instanceStorageBuffer != nil {
			r.instanceStorageBuffer.Destroy()
		}
		if r.instanceStorageBindGroup != nil {
			r.instanceStorageBindGroup.Release()
		}

		if r.instanceStorageCapacity == 0 {
			r.instanceStorageCapacity = InitialStorageCapacity
		}

		for r.instanceStorageCapacity < needInstances {
			r.instanceStorageCapacity *= 2
		}

		r.instanceStorageBuffer = r.runtime.Device.CreateBuffer(wgpu.BufferDescriptor{
			Label: "Instance storage buffer",
			Size:  uint64(r.instanceStorageCapacity) * uint64(unsafe.Sizeof(InstanceUniform{})),
			Usage: wgpu.BufferUsageStorage | wgpu.BufferUsageCopyDst,
		})

		r.instanceStorageBindGroup = r.runtime.Device.CreateBindGroup(wgpu.BindGroupDescriptor{
			Label:  "Instance bind group",
			Layout: r.instanceStorageBindGroupLayout,
			Entries: []wgpu.BindGroupEntry{
				{
					Binding: InstancesBinding,
					Buffer:  r.instanceStorageBuffer,
					Offset:  0,
					Size:    wgpu.WholeSize,
				},
			},
		})
	}
}

func (r *Renderer) ensureDepthTextureSize(width, height uint32) {
	if r.depthTexture != nil && r.depthTexture.GetWidth() == width && r.depthTexture.GetHeight() == r.height {
		return
	}

	if r.depthTexture != nil {
		r.depthTextureView.Release()
		r.depthTexture.Destroy()
	}

	r.depthTexture = r.runtime.Device.CreateTexture(&wgpu.TextureDescriptor{
		Label:         "Depth Texture",
		Usage:         wgpu.TextureUsageRenderAttachment,
		Dimension:     wgpu.TextureDimension2D,
		MipLevelCount: 1,
		SampleCount:   1,
		Format:        wgpu.TextureFormatDepth24Plus,
		Size: wgpu.Extent3D{
			Width:              width,
			Height:             height,
			DepthOrArrayLayers: 1,
		},
	})

	r.depthTextureView = r.depthTexture.CreateView(nil)
}

type renderList struct {
	visible           []drawing
	shadowCasters     []drawing
	directionalLights []directionalLightData
	ambientLight      *ambientLightData
}

func (o *renderList) release() {
	o.visible = o.visible[:0]
	o.shadowCasters = o.shadowCasters[:0]
	o.directionalLights = o.directionalLights[:0]
	o.ambientLight = nil
	renderListPool.Put(o)
}

func (r *Renderer) Render(scene *Scene, camera Camera) {
	var ctx renderContext

	r.acquireNextFrame(&ctx)
	r.Stats.NextFrame()
	start := time.Now()

	scene.UpdateTransforms()
	scene.UpdateVisibility()

	list := renderListPool.Get().(*renderList)
	defer list.release()

	viewProjection := camera.ViewProjection()
	frustum := NewFrustumFromViewProjection(viewProjection)
	r.collectRenderList(list, scene, frustum)

	instances := instancesPool.Get().(InstancesUniform)
	defer instancesPool.Put(instances[:0])

	var useLights bool

	validVisible := 0
	for i := range list.visible {
		d := &list.visible[i]
		if d.geo.gpuVersion < d.geo.version {
			if d.geo.gpuVersion == 0 {
				d.geo.gpuLayout = createVertexLayout(d.geo)
			}
			r.uploadGeometry(d.geo)
		}

		if err := prepareMaterial(r.runtime.Device, d.mat, r); err != nil {
			r.logger.Error("error preparing material", slog.Any("err", err))
			continue
		}

		if d.mat.isLit {
			useLights = true
		}

		d.instanceId = uint32(len(instances))
		instances = append(instances, InstanceUniform{d.model, d.modelInv})
		list.visible[validVisible] = *d
		validVisible++
	}
	list.visible = list.visible[:validVisible]

	for i := range list.shadowCasters {
		d := &list.shadowCasters[i]
		if d.geo.gpuVersion < d.geo.version {
			if d.geo.gpuVersion == 0 {
				d.geo.gpuLayout = createVertexLayout(d.geo)
			}
			r.uploadGeometry(d.geo)
		}

		d.instanceId = uint32(len(instances))
		instances = append(instances, InstanceUniform{d.model, d.modelInv})
	}

	if count := len(instances); count > 0 {
		r.ensureInstanceStorageSize(uint32(count))
		r.runtime.Queue.WriteBuffer(r.instanceStorageBuffer, 0, instances.Bytes())
	}

	cameraUniform := CameraUniform{
		viewProj: viewProjection,
		position: camera.Position().Vec4(),
	}
	r.runtime.Queue.WriteBuffer(r.cameraUniformBuffer, 0, cameraUniform.Bytes())

	if useLights {
		var lightsUniform LightsUniform

		count := min(MaxDirectionalLights, len(list.directionalLights))
		lightsUniform.DirectionalLightCount = uint32(count)

		for i, ld := range list.directionalLights[:count] {
			colorRGBA := ld.color.RGBA()
			colorRGBA[3] = ld.intensity

			var lightSpaceMat glm.Mat4f
			var castsShadow uint32
			var shadowBias float32

			if ld.shadow != nil {
				// Sync shadow camera to the light's current world position.
				w := scene.world[ld.ownerNode]
				ld.shadow.camera.SetPosition(glm.Vec3f{w[12], w[13], w[14]})
				ld.shadow.camera.SetTarget(ld.target)
				lightSpaceMat = ld.shadow.camera.ViewProjection()
				castsShadow = 1
				shadowBias = ld.shadow.bias
			}

			// Direction from world position to target.
			w := scene.world[ld.ownerNode]
			worldPos := glm.Vec3f{w[12], w[13], w[14]}
			lightsUniform.DirectionalLights[i] = DirectionalLightUniform{
				color:            colorRGBA,
				direction:        ld.target.Sub(worldPos).Normalize().Vec4(),
				lightSpaceMatrix: lightSpaceMat,
				castsShadow:      castsShadow,
				shadowBias:       shadowBias,
			}

			if ld.shadow != nil {
				lightFrustum := NewFrustumFromViewProjection(lightSpaceMat)
				shadowDrawings := drawingsPool.Get().([]drawing)
				for _, caster := range list.shadowCasters {
					if lightFrustum.ContainsSphere(caster.bounds) {
						shadowDrawings = append(shadowDrawings, caster)
					}
				}
				r.renderShadowMap(&ctx, ld.shadow.camera, ld.shadow.target, shadowDrawings)
				drawingsPool.Put(shadowDrawings[:0])
			}
		}

		if list.ambientLight != nil {
			lightsUniform.AmbientLight = AmbientLightUniform{
				color:     list.ambientLight.color.RGBA(),
				intensity: list.ambientLight.intensity,
			}
		}

		r.runtime.Queue.WriteBuffer(r.lightsUniformBuffer, 0, lightsUniform.Bytes())
	}

	renderPass := r.beginRendering(&ctx, scene.background)

	for _, d := range list.visible {
		r.renderInstance(renderPass, d)
	}

	r.endRendering(&ctx, renderPass)
	r.Stats.AddFrameTime(time.Since(start).Seconds())

	r.presentFrame(&ctx)
	r.drainDeferredFree()
}

func (r *Renderer) renderShadowMap(ctx *renderContext, shadowCam Camera, renderTarget *wgpu.TextureView, drawings []drawing) {
	vp := shadowCam.ViewProjection()
	r.runtime.Queue.WriteBuffer(r.shadowCamBuffer, 0,
		unsafe.Slice((*byte)(unsafe.Pointer(&vp)), unsafe.Sizeof(vp)))

	pass := ctx.encoder.BeginRenderPass(wgpu.RenderPassDescriptor{
		DepthStencilAttachment: &wgpu.RenderPassDepthStencilAttachment{
			View:            renderTarget,
			DepthLoadOp:     wgpu.LoadOpClear,
			DepthStoreOp:    wgpu.StoreOpStore,
			DepthClearValue: 1.0,
		},
	})

	pass.SetPipeline(r.shadowPipeline)
	pass.SetBindGroup(0, r.shadowCamBG, []uint32{})
	pass.SetBindGroup(1, r.instanceStorageBindGroup, []uint32{})

	for _, d := range drawings {
		for _, b := range d.geo.gpuBufs {
			if b.loc == PositionLocation {
				pass.SetVertexBuffer(0, b.buf, 0, wgpu.WholeSize)
				break
			}
		}

		if d.geo.gpuIndex != nil {
			pass.SetIndexBuffer(d.geo.gpuIndex, wgpu.IndexFormatUint32, 0, wgpu.WholeSize)
			pass.DrawIndexed(uint32(d.geo.gpuCount), 1, 0, 0, d.instanceId)
		} else {
			pass.Draw(uint32(d.geo.gpuCount), 1, 0, d.instanceId)
		}
	}

	pass.End()
	pass.Release()
}

func (r *Renderer) renderInstance(pass *wgpu.RenderPassEncoder, obj drawing) {
	pipeline := r.getPipelineFor(obj)
	pass.SetPipeline(pipeline)
	pass.SetBindGroup(GlobalSet, r.globalBindGroup, []uint32{})
	pass.SetBindGroup(MaterialSet, obj.mat.gpuBindGroup, []uint32{})
	pass.SetBindGroup(InstanceSet, r.instanceStorageBindGroup, []uint32{})

	for _, b := range obj.geo.gpuBufs {
		pass.SetVertexBuffer(uint32(b.loc), b.buf, 0, wgpu.WholeSize)
	}

	if obj.geo.gpuIndex != nil {
		pass.SetIndexBuffer(obj.geo.gpuIndex, wgpu.IndexFormatUint32, 0, wgpu.WholeSize)
		pass.DrawIndexed(uint32(obj.geo.gpuCount), 1, 0, 0, obj.instanceId)
	} else {
		pass.Draw(uint32(obj.geo.gpuCount), 1, 0, obj.instanceId)
	}
}

func (r *Renderer) acquireNextFrame(ctx *renderContext) {
	ctx.texture = r.runtime.Surface.GetCurrentTexture()
	ctx.view = ctx.texture.CreateView(nil)
	ctx.encoder = r.runtime.Device.CreateCommandEncoder(nil)
}

func (r *Renderer) presentFrame(ctx *renderContext) {
	r.runtime.Surface.Present()
	ctx.view.Release()
	ctx.view = nil

	ctx.texture.Destroy()
	ctx.texture = nil
}

func (r *Renderer) beginRendering(ctx *renderContext, bgColor glm.Color4f) *wgpu.RenderPassEncoder {
	r.ensureDepthTextureSize(ctx.texture.GetWidth(), ctx.texture.GetHeight())

	return ctx.encoder.BeginRenderPass(wgpu.RenderPassDescriptor{
		ColorAttachments: []wgpu.RenderPassColorAttachment{{
			View:       ctx.view,
			LoadOp:     wgpu.LoadOpClear,
			StoreOp:    wgpu.StoreOpStore,
			ClearValue: wgpu.Color{R: float64(bgColor.R()), G: float64(bgColor.G()), B: float64(bgColor.B()), A: float64(bgColor.A())},
		}},
		DepthStencilAttachment: &wgpu.RenderPassDepthStencilAttachment{
			View:            r.depthTextureView,
			DepthLoadOp:     wgpu.LoadOpClear,
			DepthStoreOp:    wgpu.StoreOpStore,
			DepthClearValue: 1.0,
		},
	})
}

func (r *Renderer) endRendering(ctx *renderContext, pass *wgpu.RenderPassEncoder) {
	pass.End()
	pass.Release()

	cmdBuf := ctx.encoder.Finish(nil)
	r.runtime.Queue.Submit(cmdBuf)

	cmdBuf.Release()
	ctx.encoder.Release()
	ctx.encoder = nil
}

func (r *Renderer) getPipelineFor(obj drawing) *wgpu.RenderPipeline {
	pipelineKey := renderPipelineKey{
		shaderHash:    obj.mat.hash,
		materialFlags: obj.mat.flags,
		geometryFlags: obj.geo.flags,
	}
	pipeline := r.pipelineCache.GetRenderPipeline(pipelineKey)
	if pipeline != nil {
		return pipeline
	}

	pipeline = r.createRenderPipeline(obj)
	r.pipelineCache.SetRenderPipeline(pipelineKey, pipeline)
	return pipeline
}

func (r *Renderer) createRenderPipeline(obj drawing) *wgpu.RenderPipeline {
	layout := r.runtime.Device.CreatePipelineLayout(wgpu.PipelineLayoutDescriptor{
		Label: "",
		BindGroupLayouts: []*wgpu.BindGroupLayout{
			r.globalBindGroupLayout,
			obj.mat.gpuBindGroupLayout,
			r.instanceStorageBindGroupLayout,
		},
	})
	defer layout.Release()

	defines := buildDefines(obj.mat.flags, obj.geo.flags)
	module := r.compileShader(r.runtime.Device, obj.mat.shader, defines)

	pipeline := r.runtime.Device.CreateRenderPipeline(wgpu.RenderPipelineDescriptor{
		Label:  "",
		Layout: layout,
		Vertex: wgpu.VertexState{
			Module:     module,
			EntryPoint: "vs_main",
			Buffers:    obj.geo.gpuLayout,
		},
		Fragment: &wgpu.FragmentState{
			Module:     module,
			EntryPoint: "fs_main",
			Targets: []wgpu.ColorTargetState{
				{
					Format:    r.runtime.Format,
					Blend:     nil,
					WriteMask: wgpu.ColorWriteMaskAll,
				},
			},
		},
		Primitive: wgpu.PrimitiveState{
			Topology:  wgpu.PrimitiveTopologyTriangleList,
			FrontFace: wgpu.FrontFaceCCW,
			CullMode:  wgpu.CullModeBack,
		},
		DepthStencil: &wgpu.DepthStencilState{
			Format:            wgpu.TextureFormatDepth24Plus,
			DepthWriteEnabled: wgpu.OptionalBoolTrue,
			DepthCompare:      wgpu.CompareFunctionLess,
			StencilFront: wgpu.StencilFaceState{
				Compare:     wgpu.CompareFunctionAlways,
				FailOp:      wgpu.StencilOperationKeep,
				DepthFailOp: wgpu.StencilOperationKeep,
				PassOp:      wgpu.StencilOperationKeep,
			},
			StencilBack: wgpu.StencilFaceState{
				Compare:     wgpu.CompareFunctionAlways,
				FailOp:      wgpu.StencilOperationKeep,
				DepthFailOp: wgpu.StencilOperationKeep,
				PassOp:      wgpu.StencilOperationKeep,
			},
			StencilReadMask:  0xFFFFFFFF,
			StencilWriteMask: 0xFFFFFFFF,
		},
		Multisample: wgpu.MultisampleState{
			Count:                  1,
			Mask:                   0xFFFFFFFF,
			AlphaToCoverageEnabled: false,
		},
	})

	return pipeline
}

func (r *Renderer) compileShader(device *wgpu.Device, code string, defines map[string]bool) *wgpu.ShaderModule {
	compiled, err := r.shaders.Compile(code, defines)
	if err != nil {
		r.logger.Error("shader compilation failed", slog.Any("err", err))
		compiled = code
	}

	return device.CreateShaderModule(wgpu.ShaderModuleDescriptor{
		WGSLSource: &wgpu.ShaderSourceWGSL{Code: compiled},
	})
}

func (r *Renderer) createGlobalResources() {
	r.createShadowResources()
	r.createGlobalBindGroupLayouts()
	r.createGlobalBuffers()
	r.createGlobalBindGroups()
	r.createShadowPipeline()
}

func (r *Renderer) createShadowResources() {
	r.shadowMapTexture = r.runtime.Device.CreateTexture(&wgpu.TextureDescriptor{
		Label:         "Shadow Map Array",
		Usage:         wgpu.TextureUsageRenderAttachment | wgpu.TextureUsageTextureBinding,
		Dimension:     wgpu.TextureDimension2D,
		Format:        wgpu.TextureFormatDepth32Float,
		MipLevelCount: 1,
		SampleCount:   1,
		Size: wgpu.Extent3D{
			Width:              DefaultShadowMapSize,
			Height:             DefaultShadowMapSize,
			DepthOrArrayLayers: MaxDirectionalLights,
		},
	})

	r.shadowMapView = r.shadowMapTexture.CreateView(&wgpu.TextureViewDescriptor{
		Format:          wgpu.TextureFormatDepth32Float,
		Dimension:       wgpu.TextureViewDimension2DArray,
		BaseMipLevel:    0,
		MipLevelCount:   1,
		BaseArrayLayer:  0,
		ArrayLayerCount: MaxDirectionalLights,
		Aspect:          wgpu.TextureAspectDepthOnly,
	})

	for i := range r.shadowMapLayerViews {
		r.shadowMapLayerViews[i] = r.shadowMapTexture.CreateView(&wgpu.TextureViewDescriptor{
			Format:          wgpu.TextureFormatDepth32Float,
			Dimension:       wgpu.TextureViewDimension2D,
			BaseMipLevel:    0,
			MipLevelCount:   1,
			BaseArrayLayer:  uint32(i),
			ArrayLayerCount: 1,
			Aspect:          wgpu.TextureAspectDepthOnly,
		})
	}

	r.shadowSampler = r.runtime.Device.CreateSampler(&wgpu.SamplerDescriptor{
		Label:         "Shadow Comparison Sampler",
		AddressModeU:  wgpu.AddressModeClampToEdge,
		AddressModeV:  wgpu.AddressModeClampToEdge,
		AddressModeW:  wgpu.AddressModeClampToEdge,
		MagFilter:     wgpu.FilterModeNearest,
		MinFilter:     wgpu.FilterModeNearest,
		MipmapFilter:  wgpu.MipmapFilterModeNearest,
		LodMaxClamp:   32,
		Compare:       wgpu.CompareFunctionLessEqual,
		MaxAnisotropy: 1,
	})

	r.shadowCamBuffer = r.runtime.Device.CreateBuffer(wgpu.BufferDescriptor{
		Label: "Shadow Camera Buffer",
		Size:  uint64(unsafe.Sizeof(glm.Mat4f{})),
		Usage: wgpu.BufferUsageUniform | wgpu.BufferUsageCopyDst,
	})

	r.shadowCamBGL = r.runtime.Device.CreateBindGroupLayout(wgpu.BindGroupLayoutDescriptor{
		Label: "Shadow Camera BGL",
		Entries: []wgpu.BindGroupLayoutEntry{
			{
				Binding:    0,
				Visibility: wgpu.ShaderStageVertex,
				Buffer: wgpu.BufferBindingLayout{
					Type:           wgpu.BufferBindingTypeUniform,
					MinBindingSize: uint64(unsafe.Sizeof(glm.Mat4f{})),
				},
			},
		},
	})

	r.shadowCamBG = r.runtime.Device.CreateBindGroup(wgpu.BindGroupDescriptor{
		Label:  "Shadow Camera BG",
		Layout: r.shadowCamBGL,
		Entries: []wgpu.BindGroupEntry{
			{
				Binding: 0,
				Buffer:  r.shadowCamBuffer,
				Offset:  0,
				Size:    wgpu.WholeSize,
			},
		},
	})
}

func (r *Renderer) createShadowPipeline() {
	module := r.compileShader(r.runtime.Device, "shadow.wgsl", nil)

	layout := r.runtime.Device.CreatePipelineLayout(wgpu.PipelineLayoutDescriptor{
		Label: "Shadow Pipeline Layout",
		BindGroupLayouts: []*wgpu.BindGroupLayout{
			r.shadowCamBGL,
			r.instanceStorageBindGroupLayout,
		},
	})
	defer layout.Release()

	r.shadowPipeline = r.runtime.Device.CreateRenderPipeline(wgpu.RenderPipelineDescriptor{
		Label:  "Shadow Pipeline",
		Layout: layout,
		Vertex: wgpu.VertexState{
			Module:     module,
			EntryPoint: "vs_shadow",
			Buffers: []wgpu.VertexBufferLayout{
				{
					ArrayStride: uint64(Float32x3.Size()),
					StepMode:    wgpu.VertexStepModeVertex,
					Attributes: []wgpu.VertexAttribute{
						{
							Format:         wgpu.VertexFormatFloat32x3,
							Offset:         0,
							ShaderLocation: 0,
						},
					},
				},
			},
		},
		Primitive: wgpu.PrimitiveState{
			Topology:  wgpu.PrimitiveTopologyTriangleList,
			FrontFace: wgpu.FrontFaceCCW,
			CullMode:  wgpu.CullModeFront, // render back faces to reduce self-shadowing
		},
		DepthStencil: &wgpu.DepthStencilState{
			Format:            wgpu.TextureFormatDepth32Float,
			DepthWriteEnabled: wgpu.OptionalBoolTrue,
			DepthCompare:      wgpu.CompareFunctionLessEqual,
			StencilFront: wgpu.StencilFaceState{
				Compare:     wgpu.CompareFunctionAlways,
				FailOp:      wgpu.StencilOperationKeep,
				DepthFailOp: wgpu.StencilOperationKeep,
				PassOp:      wgpu.StencilOperationKeep,
			},
			StencilBack: wgpu.StencilFaceState{
				Compare:     wgpu.CompareFunctionAlways,
				FailOp:      wgpu.StencilOperationKeep,
				DepthFailOp: wgpu.StencilOperationKeep,
				PassOp:      wgpu.StencilOperationKeep,
			},
			StencilReadMask:  0xFFFFFFFF,
			StencilWriteMask: 0xFFFFFFFF,
		},
		Multisample: wgpu.MultisampleState{
			Count:                  1,
			Mask:                   0xFFFFFFFF,
			AlphaToCoverageEnabled: false,
		},
	})
}

func (r *Renderer) createGlobalBindGroupLayouts() {
	r.globalBindGroupLayout = r.runtime.Device.CreateBindGroupLayout(wgpu.BindGroupLayoutDescriptor{
		Label: "Global Bind Group Layout",
		Entries: []wgpu.BindGroupLayoutEntry{
			{
				Binding:    CameraBinding,
				Visibility: wgpu.ShaderStageVertex | wgpu.ShaderStageFragment,
				Buffer: wgpu.BufferBindingLayout{
					Type:             wgpu.BufferBindingTypeUniform,
					HasDynamicOffset: false,
					MinBindingSize:   uint64(unsafe.Sizeof(CameraUniform{})),
				},
			},
			{
				Binding:    LightsBinding,
				Visibility: wgpu.ShaderStageVertex | wgpu.ShaderStageFragment,
				Buffer: wgpu.BufferBindingLayout{
					Type:             wgpu.BufferBindingTypeUniform,
					HasDynamicOffset: false,
					MinBindingSize:   uint64(unsafe.Sizeof(LightsUniform{})),
				},
			},
			{
				Binding:    ShadowMapBinding,
				Visibility: wgpu.ShaderStageFragment,
				Texture: wgpu.TextureBindingLayout{
					SampleType:    wgpu.TextureSampleTypeDepth,
					ViewDimension: wgpu.TextureViewDimension2DArray,
					Multisampled:  false,
				},
			},
			{
				Binding:    ShadowSamplerBinding,
				Visibility: wgpu.ShaderStageFragment,
				Sampler: wgpu.SamplerBindingLayout{
					Type: wgpu.SamplerBindingTypeComparison,
				},
			},
		},
	})

	r.instanceStorageBindGroupLayout = r.runtime.Device.CreateBindGroupLayout(wgpu.BindGroupLayoutDescriptor{
		Label: "Instance/Model Bind Group Layout",
		Entries: []wgpu.BindGroupLayoutEntry{
			{
				Binding:    InstancesBinding,
				Visibility: wgpu.ShaderStageVertex | wgpu.ShaderStageFragment,
				Buffer: wgpu.BufferBindingLayout{
					Type:             wgpu.BufferBindingTypeReadOnlyStorage,
					HasDynamicOffset: false,
					MinBindingSize:   uint64(unsafe.Sizeof(InstanceUniform{})),
				},
			},
		},
	})
}

func (r *Renderer) createGlobalBuffers() {
	r.cameraUniformBuffer = r.runtime.Device.CreateBuffer(wgpu.BufferDescriptor{
		Label: "Camera uniform buffer",
		Size:  uint64(unsafe.Sizeof(CameraUniform{})),
		Usage: wgpu.BufferUsageUniform | wgpu.BufferUsageCopyDst,
	})

	r.lightsUniformBuffer = r.runtime.Device.CreateBuffer(wgpu.BufferDescriptor{
		Label: "Lights uniform buffer",
		Size:  uint64(unsafe.Sizeof(LightsUniform{})),
		Usage: wgpu.BufferUsageUniform | wgpu.BufferUsageCopyDst,
	})

	r.ensureInstanceStorageSize(InitialStorageCapacity)
}

func (r *Renderer) createGlobalBindGroups() {
	r.globalBindGroup = r.runtime.Device.CreateBindGroup(wgpu.BindGroupDescriptor{
		Label:  "Global bind group",
		Layout: r.globalBindGroupLayout,
		Entries: []wgpu.BindGroupEntry{
			{
				Binding: CameraBinding,
				Buffer:  r.cameraUniformBuffer,
				Offset:  0,
				Size:    wgpu.WholeSize,
			},
			{
				Binding: LightsBinding,
				Buffer:  r.lightsUniformBuffer,
				Offset:  0,
				Size:    wgpu.WholeSize,
			},
			{
				Binding:     ShadowMapBinding,
				TextureView: r.shadowMapView,
			},
			{
				Binding: ShadowSamplerBinding,
				Sampler: r.shadowSampler,
			},
		},
	})
}

// collectRenderList populates list by iterating the scene's compact payload tables
// directly, avoiding a full tree traversal.
func (r *Renderer) collectRenderList(list *renderList, scene *Scene, frustum Frustum) {
	for _, md := range scene.meshes {
		flags := scene.GetFlags(md.ownerNode)
		if !flags.IsAlive() || !flags.IsVisible() {
			continue
		}

		d := drawing{
			geo:      r.geometries.get(md.geometry.ref.ID()),
			mat:      r.materials.get(md.material.ref.ID()),
			model:    scene.GetWorldTransform(md.ownerNode),
			modelInv: scene.GetWorldTransformInv(md.ownerNode),
			bounds:   md.boundingSphere,
		}

		if flags.CastShadow() {
			list.shadowCasters = append(list.shadowCasters, d)
		}
		if frustum.ContainsSphere(d.bounds) {
			list.visible = append(list.visible, d)
		}
	}

	shadowLayer := 0
	for i := range scene.dirLights {
		ld := scene.dirLights[i]
		if scene.flags[ld.ownerNode]&flagAlive == 0 {
			continue
		}
		if ld.shadow != nil && shadowLayer < MaxDirectionalLights {
			if ld.shadow.target == nil {
				ld.shadow.target = r.shadowMapLayerViews[shadowLayer]
			}
			shadowLayer++
		}
		list.directionalLights = append(list.directionalLights, ld)
	}

	for i := range scene.ambientLights {
		ld := &scene.ambientLights[i]
		if scene.flags[ld.ownerNode]&flagAlive == 0 {
			continue
		}
		list.ambientLight = ld
		break
	}
}

func buildDefines(matFlags MaterialFlags, geoFlags GeometryFlags) map[string]bool {
	var defines = make(map[string]bool)

	for flags := matFlags; flags != 0; {
		bit := bits.TrailingZeros64(uint64(flags))
		flags &= flags - 1
		if name, ok := materialFlagNames[bit]; ok {
			defines[name] = true
		}
	}

	for flags := geoFlags; flags != 0; {
		bit := bits.TrailingZeros64(uint64(flags))
		flags &= flags - 1
		if name, ok := geometryFlagNames[bit]; ok {
			defines[name] = true
		}
	}

	return defines
}
