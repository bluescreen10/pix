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

var viewableMeshesPool = sync.Pool{
	New: func() any {
		return make([]*Mesh, 0, 4096)
	},
}

var viewableDirectionalLights = sync.Pool{
	New: func() any {
		return make([]*DirectionalLight, 0, MaxDirectionalLights)
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
	resources resourceManager

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

	r.resources.init()
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

	r.resources.destroy()
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
	meshes            []*Mesh
	directionalLights []*DirectionalLight
	ambientLight      *AmbientLight
}

func (o *renderList) init() {
	o.meshes = viewableMeshesPool.Get().([]*Mesh)
	o.directionalLights = viewableDirectionalLights.Get().([]*DirectionalLight)
	o.ambientLight = nil
}

func (o *renderList) release() {
	viewableMeshesPool.Put(o.meshes[:0])
	viewableDirectionalLights.Put(o.directionalLights[:0])
}

func (r *Renderer) Render(scene *Scene, camera Camera) {
	var ctx renderContext

	r.acquireNextFrame(&ctx)
	r.Stats.NextFrame()
	start := time.Now()

	updateMatrix(scene, false)

	var list renderList
	list.init()
	defer list.release()

	viewProjection := camera.ViewProjection()
	frustum := NewFrustumFromViewProjection(viewProjection)
	r.cullScene(&list, scene, frustum)

	drawings := drawingsPool.Get().([]drawing)
	defer drawingsPool.Put(drawings[:0])

	instances := instancesPool.Get().(InstancesUniform)
	defer instancesPool.Put(instances[:0])

	var useLights bool

	for _, mesh := range list.meshes {
		geometryData := mesh.geometry
		geometry := r.resources.GetGeometry(geometryData)

		if geometry.version < geometryData.version {
			if geometry.version == 0 {
				geometry.layout = createVertexLayout(geometryData)
			}
			err := r.resources.uploadGeometry(r.runtime.Device, geometryData)
			if err != nil {
				r.logger.Error("error uploading geometry", slog.Any("err", err))
			}
		}

		materialData := mesh.material
		material := r.resources.GetMaterialByData(materialData)
		material, err := prepareMaterial(r.runtime.Device, materialData, material, &r.resources)

		if err != nil {
			r.logger.Error("error preparing material", slog.Any("err", err))
			continue
		}

		r.resources.SetMaterial(materialData.slot, material)

		if material.isLit {
			useLights = true
		}

		drawings = append(drawings, drawing{
			geometry: *geometry,
			material: material,
		})

		instances = append(instances, InstanceUniform{mesh.Model(), mesh.InvModel()})
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

		for i, l := range list.directionalLights[:count] {
			colorRGBA := l.color.RGBA()
			colorRGBA[3] = l.intensity

			var lightSpaceMat glm.Mat4f
			var castsShadow uint32
			var shadowBias float32

			if l.shadow != nil {
				l.shadow.Camera.SetPosition(l.pos)
				l.shadow.Camera.SetTarget(l.target)
				lightSpaceMat = l.shadow.Camera.ViewProjection()
				castsShadow = 1
				shadowBias = l.shadow.Bias
			}

			lightsUniform.DirectionalLights[i] = DirectionalLightUniform{
				color:            colorRGBA,
				direction:        l.target.Sub(l.pos).Normalize().Vec4(),
				lightSpaceMatrix: lightSpaceMat,
				castsShadow:      castsShadow,
				shadowBias:       shadowBias,
			}
		}

		if list.ambientLight != nil {
			lightsUniform.AmbientLight = AmbientLightUniform{
				color:     list.ambientLight.color.RGBA(),
				intensity: list.ambientLight.intensity,
			}
		}

		r.runtime.Queue.WriteBuffer(r.lightsUniformBuffer, 0, lightsUniform.Bytes())

		r.renderShadowMaps(&list, drawings)
	}

	renderPass := r.beginRendering(&ctx, scene.background)

	for i, drawing := range drawings {
		r.renderInstance(renderPass, drawing, i)
	}

	r.endRendering(&ctx, renderPass)
	r.Stats.AddFrameTime(time.Since(start).Seconds())

	r.presentFrame(&ctx)
	r.resources.processPending(r.runtime.Device)
}

func (r *Renderer) renderShadowMaps(list *renderList, drawings []drawing) {
	count := min(MaxDirectionalLights, len(list.directionalLights))

	for i, light := range list.directionalLights[:count] {
		if light.shadow == nil {
			continue
		}

		vp := light.shadow.Camera.ViewProjection()
		r.runtime.Queue.WriteBuffer(r.shadowCamBuffer, 0,
			unsafe.Slice((*byte)(unsafe.Pointer(&vp)), unsafe.Sizeof(vp)))

		encoder := r.runtime.Device.CreateCommandEncoder(nil)
		pass := encoder.BeginRenderPass(wgpu.RenderPassDescriptor{
			DepthStencilAttachment: &wgpu.RenderPassDepthStencilAttachment{
				View:            r.shadowMapLayerViews[i],
				DepthLoadOp:     wgpu.LoadOpClear,
				DepthStoreOp:    wgpu.StoreOpStore,
				DepthClearValue: 1.0,
			},
		})

		pass.SetPipeline(r.shadowPipeline)
		pass.SetBindGroup(0, r.shadowCamBG, []uint32{})
		pass.SetBindGroup(1, r.instanceStorageBindGroup, []uint32{})

		for j, d := range drawings {
			for _, b := range d.geometry.bufs {
				if b.loc == PositionLocation {
					pass.SetVertexBuffer(0, b.buf, 0, wgpu.WholeSize)
					break
				}
			}

			if d.geometry.index != nil {
				pass.SetIndexBuffer(d.geometry.index, wgpu.IndexFormatUint32, 0, wgpu.WholeSize)
				pass.DrawIndexed(uint32(d.geometry.count), 1, 0, 0, uint32(j))
			} else {
				pass.Draw(uint32(d.geometry.count), 1, 0, uint32(j))
			}
		}

		pass.End()
		pass.Release()

		cmdBuf := encoder.Finish(nil)
		r.runtime.Queue.Submit(cmdBuf)
		cmdBuf.Release()
		encoder.Release()
	}
}

func (r *Renderer) renderInstance(pass *wgpu.RenderPassEncoder, obj drawing, objIdx int) {
	pipeline := r.getPipelineFor(obj)
	pass.SetPipeline(pipeline)
	pass.SetBindGroup(GlobalSet, r.globalBindGroup, []uint32{})
	pass.SetBindGroup(MaterialSet, obj.material.bindGroup, []uint32{})
	pass.SetBindGroup(InstanceSet, r.instanceStorageBindGroup, []uint32{})

	for _, b := range obj.geometry.bufs {
		pass.SetVertexBuffer(uint32(b.loc), b.buf, 0, wgpu.WholeSize)
	}

	if obj.geometry.index != nil {
		pass.SetIndexBuffer(obj.geometry.index, wgpu.IndexFormatUint32, 0, wgpu.WholeSize)
		pass.DrawIndexed(uint32(obj.geometry.count), 1, 0, 0, uint32(objIdx))
	} else {
		pass.Draw(uint32(obj.geometry.count), 1, 0, uint32(objIdx))
	}
}

func (r *Renderer) acquireNextFrame(ctx *renderContext) {
	ctx.texture = r.runtime.Surface.GetCurrentTexture()
	ctx.view = ctx.texture.CreateView(nil)
}

func (r *Renderer) presentFrame(ctx *renderContext) {
	r.runtime.Surface.Present()
	ctx.view.Release()
	ctx.view = nil

	ctx.texture.Destroy()
	ctx.texture = nil
}

func (r *Renderer) beginRendering(ctx *renderContext, bgColor glm.Color4f) *wgpu.RenderPassEncoder {
	ctx.encoder = r.runtime.Device.CreateCommandEncoder(nil)

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
		shaderHash:    obj.material.hash,
		materialFlags: obj.material.flags,
		geometryFlags: obj.geometry.flags,
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
			obj.material.bindGroupLayout,
			r.instanceStorageBindGroupLayout,
		},
	})

	defines := buildDefines(obj.material.flags, obj.geometry.flags)
	module := r.compileShader(r.runtime.Device, obj.material.shader, defines)

	pipeline := r.runtime.Device.CreateRenderPipeline(wgpu.RenderPipelineDescriptor{
		Label:  "",
		Layout: layout,
		Vertex: wgpu.VertexState{
			Module:     module,
			EntryPoint: "vs_main",
			Buffers:    obj.geometry.layout,
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

func (r *Renderer) cullScene(list *renderList, node Node, frustum Frustum) {
	for _, child := range node.Children() {
		switch object := any(child).(type) {
		case *Mesh:
			if frustum.ContainsSphere(object.BoundingSphere()) {
				list.meshes = append(list.meshes, object)
			}
		case *DirectionalLight:
			list.directionalLights = append(list.directionalLights, object)
		case *AmbientLight:
			if list.ambientLight == nil {
				list.ambientLight = object
			}
		}

		r.cullScene(list, child, frustum)
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

func updateMatrix(n Node, force bool) {
	force = n.UpdateMatrix(force)
	for _, child := range n.Children() {
		updateMatrix(child, force)
	}
}

func transformSphere(sphere Sphere, model glm.Mat4f) Sphere {
	worldCenter := model.Mul4x1(glm.Vec4f{sphere.Center[0], sphere.Center[1], sphere.Center[2], 1.0})

	sx := glm.Vec3f{model[0], model[1], model[2]}.Length()
	sy := glm.Vec3f{model[4], model[5], model[6]}.Length()
	sz := glm.Vec3f{model[8], model[9], model[10]}.Length()

	maxScale := max(sx, max(sy, sz))

	return Sphere{
		Center: glm.Vec3f{worldCenter[0], worldCenter[1], worldCenter[2]},
		Radius: sphere.Radius * maxScale,
	}
}
