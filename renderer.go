package pix

import (
	"log/slog"
	"math"
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
	MaxSpotLights          = 5
	MaxPointLights         = 5
	DefaultShadowMapSize   = 512
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
	PointShadowMapBinding
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
	texture         *wgpu.Texture
	view            *wgpu.TextureView
	encoder         *wgpu.CommandEncoder
	depthTarget     *wgpu.Texture
	depthTargetView *wgpu.TextureView
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

	//TODO: have a max number of shadow maps (i.e. 8)
	// and let lights request (1 or multuple slots)
	// shadow (directional + spot)
	shadowMap        *TextureData
	shadowLayerViews [MaxDirectionalLights + MaxSpotLights]*wgpu.TextureView
	shadowMat        *ShadowMaterial

	// shadow (point — cube array)
	pointShadowMap        *TextureData
	pointShadowLayerViews [MaxPointLights * 6]*wgpu.TextureView
	pointShadowMat        *PointShadowMaterial

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
	if err := r.shaders.ParseFS(shaderlib); err != nil {
		panic(err)
	}
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

	if r.depthTextureView != nil {
		r.depthTextureView.Release()
		r.depthTexture = nil
	}

	if r.depthTexture != nil {
		r.depthTexture.Destroy()
		r.depthTexture = nil
	}

	for i, v := range r.shadowLayerViews {
		if v != nil {
			v.Release()
			r.shadowLayerViews[i] = nil
		}
	}

	if r.shadowMap != nil {
		r.shadowMap.Destroy()
		r.shadowMap = nil
	}

	for i, v := range r.pointShadowLayerViews {
		if v != nil {
			v.Release()
			r.pointShadowLayerViews[i] = nil
		}
	}

	if r.pointShadowMap != nil {
		r.pointShadowMap.Destroy()
		r.pointShadowMap = nil
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
	spotLights        []spotLightData
	pointLights       []pointLightData
}

func (o *renderList) release() {
	o.visible = o.visible[:0]
	o.shadowCasters = o.shadowCasters[:0]
	o.directionalLights = o.directionalLights[:0]
	o.ambientLight = nil
	o.spotLights = o.spotLights[:0]
	o.pointLights = o.pointLights[:0]
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
	r.collectRenderList(list, scene)
	list.visible = cull(frustum, list.visible, list.visible[:0])

	instances := instancesPool.Get().(InstancesUniform)
	defer instancesPool.Put(instances[:0])

	var useLights bool

	validVisible := 0
	for i := range list.visible {
		d := &list.visible[i]
		if d.geo.gpuVersion < d.geo.version {
			if d.geo.gpuVersion == 0 {
				d.geo.gpuLayout = createVertexLayout(d.geo)
				d.geo.gpuShadowLayout = createShadowVertexLayout(d.geo)
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
		instances = appendInstances(instances, d)
		list.visible[validVisible] = *d
		validVisible++
	}
	list.visible = list.visible[:validVisible]

	for i := range list.shadowCasters {
		d := &list.shadowCasters[i]
		if d.geo.gpuVersion < d.geo.version {
			if d.geo.gpuVersion == 0 {
				d.geo.gpuLayout = createVertexLayout(d.geo)
				d.geo.gpuShadowLayout = createShadowVertexLayout(d.geo)
			}
			r.uploadGeometry(d.geo)
		}

		d.instanceId = uint32(len(instances))
		instances = appendInstances(instances, d)
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

	ctx.encoder = r.runtime.Device.CreateCommandEncoder(nil)

	shadowLayerIdx := 0
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
				shadowDrawings = cull(lightFrustum, list.shadowCasters, shadowDrawings)
				ctx.depthTarget = r.shadowMap.gpuRef
				ctx.depthTargetView = r.shadowLayerViews[shadowLayerIdx]
				r.renderShadowMap(&ctx, ld.shadow.camera, shadowDrawings)
				drawingsPool.Put(shadowDrawings[:0])
				shadowLayerIdx++
			}
		}

		if list.ambientLight != nil {
			lightsUniform.AmbientLight = AmbientLightUniform{
				color:     list.ambientLight.color.RGBA(),
				intensity: list.ambientLight.intensity,
			}
		}

		spotCount := min(MaxSpotLights, len(list.spotLights))
		lightsUniform.SpotLightCount = uint32(spotCount)

		for i, ld := range list.spotLights[:spotCount] {
			colorRGBA := ld.color.RGBA()
			colorRGBA[3] = ld.intensity

			w := scene.world[ld.ownerNode]
			worldPos := glm.Vec3f{w[12], w[13], w[14]}
			spotDir := ld.target.Sub(worldPos).Normalize()

			var lightSpaceMat glm.Mat4f
			var castsShadow uint32
			var shadowBias float32

			if ld.shadow != nil {
				fwd := ld.target.Sub(worldPos).Normalize()
				up := glm.Vec3f{0, 1, 0}
				if fwd[1] > 0.999 || fwd[1] < -0.999 {
					up = glm.Vec3f{0, 0, 1}
				}
				ld.shadow.camera.SetPosition(worldPos)
				ld.shadow.camera.SetFwd(fwd)
				ld.shadow.camera.SetUp(up)
				lightSpaceMat = ld.shadow.camera.ViewProjection()
				castsShadow = 1
				shadowBias = ld.shadow.bias
			}

			innerCosine := float32(math.Cos(float64(ld.innerAngle) * math.Pi / 180.0))
			outerCosine := float32(math.Cos(float64(ld.outerAngle) * math.Pi / 180.0))

			lightsUniform.SpotLights[i] = SpotLightUniform{
				color:            colorRGBA,
				position:         glm.Vec4f{worldPos[0], worldPos[1], worldPos[2], 1},
				direction:        glm.Vec4f{spotDir[0], spotDir[1], spotDir[2], 0},
				lightSpaceMatrix: lightSpaceMat,
				innerCosine:      innerCosine,
				outerCosine:      outerCosine,
				castsShadow:      castsShadow,
				shadowBias:       shadowBias,
			}

			if ld.shadow != nil {
				lightFrustum := NewFrustumFromViewProjection(lightSpaceMat)
				shadowDrawings := drawingsPool.Get().([]drawing)
				shadowDrawings = cull(lightFrustum, list.shadowCasters, shadowDrawings)
				ctx.depthTarget = r.shadowMap.gpuRef
				ctx.depthTargetView = r.shadowLayerViews[MaxDirectionalLights+i]
				r.renderShadowMap(&ctx, ld.shadow.camera, shadowDrawings)
				drawingsPool.Put(shadowDrawings[:0])
			}
		}

		pointCount := min(MaxPointLights, len(list.pointLights))
		lightsUniform.PointLightCount = uint32(pointCount)

		for i, ld := range list.pointLights[:pointCount] {
			colorRGBA := ld.color.RGBA()
			colorRGBA[3] = ld.intensity

			w := scene.world[ld.ownerNode]
			worldPos := glm.Vec3f{w[12], w[13], w[14]}

			var castsShadow uint32
			var shadowBias float32
			var far float32 = 100

			if ld.shadow != nil {
				castsShadow = 1
				shadowBias = ld.shadow.bias
				far = ld.shadow.far
				r.renderPointShadowCube(&ctx, worldPos, ld.shadow, i, list.shadowCasters)
			}

			lightsUniform.PointLights[i] = PointLightUniform{
				color:       colorRGBA,
				position:    glm.Vec4f{worldPos[0], worldPos[1], worldPos[2], 1},
				far:         far,
				castsShadow: castsShadow,
				shadowBias:  shadowBias,
			}
		}

		r.runtime.Queue.WriteBuffer(r.lightsUniformBuffer, 0, lightsUniform.Bytes())
	}

	// All shadow passes have been submitted. Create the main encoder now so the
	// shadow texture transitions from RenderAttachment to TextureBinding are complete.

	r.ensureDepthTextureSize(ctx.texture.GetWidth(), ctx.texture.GetHeight())
	ctx.depthTarget = r.depthTexture
	ctx.depthTargetView = r.depthTextureView

	renderPass := r.beginRendering(&ctx, scene.background)

	for _, d := range list.visible {
		r.renderInstance(&ctx, renderPass, d)
	}

	r.endRendering(&ctx, renderPass)
	r.Stats.AddFrameTime(time.Since(start).Seconds())

	r.presentFrame(&ctx)
	r.drainDeferredFree()
}

func (r *Renderer) renderShadowMap(ctx *renderContext, shadowCam Camera, drawings []drawing) {
	// Each shadow pass owns its encoder so the VP WriteBuffer committed to the
	// queue before this call is the value actually used — not overwritten by the
	// next light's WriteBuffer before the shared encoder is submitted.
	// encoder := r.runtime.Device.CreateCommandEncoder(nil)
	// defer func() {
	// 	cmd := encoder.Finish(nil)
	// 	r.runtime.Queue.Submit(cmd)
	// 	cmd.Release()
	// 	encoder.Release()
	// }()

	// Always begin (and end) the pass so the layer is cleared to 1.0.
	// Without the clear, the GPU-zero-initialized texture causes every
	// shadow comparison to fail and the whole scene appears unlit.
	pass := ctx.encoder.BeginRenderPass(wgpu.RenderPassDescriptor{
		DepthStencilAttachment: &wgpu.RenderPassDepthStencilAttachment{
			View:            ctx.depthTargetView,
			DepthLoadOp:     wgpu.LoadOpClear,
			DepthStoreOp:    wgpu.StoreOpStore,
			DepthClearValue: 1.0,
		},
	})

	if len(drawings) == 0 {
		pass.End()
		pass.Release()
		return
	}

	r.shadowMat.SetViewProjection(shadowCam.ViewProjection())
	if err := prepareMaterial(r.runtime.Device, r.shadowMat.data(), r); err != nil {
		r.logger.Error("error preparing shadow material", slog.Any("err", err))
		pass.End()
		pass.Release()
		return
	}

	pass.SetBindGroup(0, r.shadowMat.BindGroup(), nil)
	pass.SetBindGroup(1, r.instanceStorageBindGroup, nil)

	for _, d := range drawings {
		shadowObj := drawing{mat: r.shadowMat.data(), geo: d.geo}
		pass.SetPipeline(r.getPipeline(shadowObj, nil, ctx.depthTarget))

		for _, b := range d.geo.gpuBufs {
			pass.SetVertexBuffer(uint32(b.loc), b.buf, 0, wgpu.WholeSize)
		}

		if d.geo.gpuIndex != nil {
			pass.SetIndexBuffer(d.geo.gpuIndex, wgpu.IndexFormatUint32, 0, wgpu.WholeSize)
			pass.DrawIndexed(uint32(d.geo.gpuCount), d.instanceCount, 0, 0, d.instanceId)
		} else {
			pass.Draw(uint32(d.geo.gpuCount), d.instanceCount, 0, d.instanceId)
		}
	}

	pass.End()
	pass.Release()
}

// cubeFaces defines the 6 view directions and up vectors for rendering cube shadow map faces.
// The ordering matches WebGPU/Vulkan cube map layers: +X, -X, +Y, -Y, +Z, -Z.
var cubeFaces = [6]struct{ fwd, up glm.Vec3f }{
	{glm.Vec3f{1, 0, 0}, glm.Vec3f{0, -1, 0}},  // +X
	{glm.Vec3f{-1, 0, 0}, glm.Vec3f{0, -1, 0}}, // -X
	{glm.Vec3f{0, 1, 0}, glm.Vec3f{0, 0, 1}},   // +Y
	{glm.Vec3f{0, -1, 0}, glm.Vec3f{0, 0, -1}}, // -Y
	{glm.Vec3f{0, 0, 1}, glm.Vec3f{0, -1, 0}},  // +Z
	{glm.Vec3f{0, 0, -1}, glm.Vec3f{0, -1, 0}}, // -Z
}

func (r *Renderer) renderPointShadowCube(ctx *renderContext, lightPos glm.Vec3f, shadow *PointShadow, lightIdx int, casters []drawing) {
	near := float32(0.1)
	proj := glm.PerspectiveRH(float32(math.Pi/2), float32(1.0), near, shadow.far)
	// Negate h (proj[5]) so rendered V matches WebGPU cube map sampling convention (tc = -ry).
	// Without this flip the cube faces are upside-down relative to textureSampleCompare.
	proj[5] = -proj[5]

	for face, cf := range cubeFaces {
		target := lightPos.Add(cf.fwd)
		view := glm.LookAtRH(lightPos, target, cf.up)
		vp := proj.Mul4x4(view)

		r.pointShadowMat.SetFaceUniforms(vp, lightPos, shadow.far)
		if err := prepareMaterial(r.runtime.Device, r.pointShadowMat.data(), r); err != nil {
			r.logger.Error("error preparing point shadow material", slog.Any("err", err))
			continue
		}

		layerIdx := lightIdx*6 + face
		ctx.depthTargetView = r.pointShadowLayerViews[layerIdx]
		ctx.depthTarget = r.pointShadowMap.gpuRef

		encoder := r.runtime.Device.CreateCommandEncoder(nil)
		pass := encoder.BeginRenderPass(wgpu.RenderPassDescriptor{
			DepthStencilAttachment: &wgpu.RenderPassDepthStencilAttachment{
				View:            ctx.depthTargetView,
				DepthLoadOp:     wgpu.LoadOpClear,
				DepthStoreOp:    wgpu.StoreOpStore,
				DepthClearValue: 1.0,
			},
		})

		if len(casters) > 0 {
			pass.SetBindGroup(0, r.pointShadowMat.BindGroup(), nil)
			pass.SetBindGroup(1, r.instanceStorageBindGroup, nil)

			for _, d := range casters {
				shadowObj := drawing{mat: r.pointShadowMat.data(), geo: d.geo}
				pass.SetPipeline(r.getPipeline(shadowObj, nil, ctx.depthTarget))
				for _, b := range d.geo.gpuBufs {
					pass.SetVertexBuffer(uint32(b.loc), b.buf, 0, wgpu.WholeSize)
				}
				if d.geo.gpuIndex != nil {
					pass.SetIndexBuffer(d.geo.gpuIndex, wgpu.IndexFormatUint32, 0, wgpu.WholeSize)
					pass.DrawIndexed(uint32(d.geo.gpuCount), d.instanceCount, 0, 0, d.instanceId)
				} else {
					pass.Draw(uint32(d.geo.gpuCount), d.instanceCount, 0, d.instanceId)
				}
			}
		}

		pass.End()
		pass.Release()
		cmd := encoder.Finish(nil)
		r.runtime.Queue.Submit(cmd)
		cmd.Release()
		encoder.Release()
	}
}

func (r *Renderer) renderInstance(ctx *renderContext, pass *wgpu.RenderPassEncoder, obj drawing) {
	pipeline := r.getPipeline(obj, ctx.texture, ctx.depthTarget)
	pass.SetPipeline(pipeline)
	pass.SetBindGroup(GlobalSet, r.globalBindGroup, nil)
	pass.SetBindGroup(MaterialSet, obj.mat.gpuBindGroup, nil)
	pass.SetBindGroup(InstanceSet, r.instanceStorageBindGroup, nil)

	for _, b := range obj.geo.gpuBufs {
		pass.SetVertexBuffer(uint32(b.loc), b.buf, 0, wgpu.WholeSize)
	}

	if obj.geo.gpuIndex != nil {
		pass.SetIndexBuffer(obj.geo.gpuIndex, wgpu.IndexFormatUint32, 0, wgpu.WholeSize)
		pass.DrawIndexed(uint32(obj.geo.gpuCount), obj.instanceCount, 0, 0, obj.instanceId)
	} else {
		pass.Draw(uint32(obj.geo.gpuCount), obj.instanceCount, 0, obj.instanceId)
	}
}

func (r *Renderer) acquireNextFrame(ctx *renderContext) {
	ctx.texture = r.runtime.Surface.GetCurrentTexture()
	ctx.view = ctx.texture.CreateView(nil)
	// Main encoder is created after all shadow passes are submitted,
	// so the shadow texture is fully written before it is bound as a sampler input.
}

func (r *Renderer) presentFrame(ctx *renderContext) {
	r.runtime.Surface.Present()
	ctx.view.Release()
	ctx.view = nil

	ctx.texture.Destroy()
	ctx.texture = nil
}

func (r *Renderer) beginRendering(ctx *renderContext, bgColor glm.Color4f) *wgpu.RenderPassEncoder {

	return ctx.encoder.BeginRenderPass(wgpu.RenderPassDescriptor{
		ColorAttachments: []wgpu.RenderPassColorAttachment{{
			View:       ctx.view,
			LoadOp:     wgpu.LoadOpClear,
			StoreOp:    wgpu.StoreOpStore,
			ClearValue: wgpu.Color{R: float64(bgColor.R()), G: float64(bgColor.G()), B: float64(bgColor.B()), A: float64(bgColor.A())},
		}},
		DepthStencilAttachment: &wgpu.RenderPassDepthStencilAttachment{
			View:            ctx.depthTargetView,
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

func (r *Renderer) getPipeline(obj drawing, renderTarget, depthTarget *wgpu.Texture) *wgpu.RenderPipeline {
	mat := obj.mat
	shadow := renderTarget == nil

	geoFlags := obj.geo.flags
	if shadow {
		geoFlags &= ShadowGeometryMask
	}

	var colorFormat wgpu.TextureFormat
	if !shadow {
		colorFormat = renderTarget.GetFormat()
	}
	key := renderPipelineKey{
		shaderHash:    mat.hash,
		materialFlags: mat.flags,
		geometryFlags: geoFlags,
		colorFormat:   colorFormat,
		depthFormat:   depthTarget.GetFormat(),
		side:          mat.side,
		blending:      mat.blending,
		depthFunc:     mat.depthFunc,
		depthWrite:    mat.depthWrite,
		depthTest:     mat.depthTest,
		colorWrite:    mat.colorWrite,
	}
	if p := r.pipelineCache.GetRenderPipeline(key); p != nil {
		return p
	}
	p := r.createPipeline(mat, obj.geo, renderTarget, depthTarget)
	r.pipelineCache.SetRenderPipeline(key, p)
	return p
}

// createPipeline builds a render pipeline. When renderTarget is nil the
// pipeline is depth-only (no fragment stage).
func (r *Renderer) createPipeline(mat *MaterialData, geo *GeometryData, renderTarget, depthTarget *wgpu.Texture) *wgpu.RenderPipeline {
	shadow := renderTarget == nil

	var bindGroupLayouts []*wgpu.BindGroupLayout
	if shadow {
		bindGroupLayouts = []*wgpu.BindGroupLayout{
			mat.gpuBindGroupLayout,
			r.instanceStorageBindGroupLayout,
		}
	} else {
		bindGroupLayouts = []*wgpu.BindGroupLayout{
			r.globalBindGroupLayout,
			mat.gpuBindGroupLayout,
			r.instanceStorageBindGroupLayout,
		}
	}

	layout := r.runtime.Device.CreatePipelineLayout(wgpu.PipelineLayoutDescriptor{
		BindGroupLayouts: bindGroupLayouts,
	})
	defer layout.Release()

	geoFlags := geo.flags
	vertexLayout := geo.gpuLayout
	if shadow {
		geoFlags = geo.flags & ShadowGeometryMask
		vertexLayout = geo.gpuShadowLayout
	}

	defines := buildDefines(mat.flags, geoFlags)

	var vertex, fragment *wgpu.ShaderModule

	if mat.vertexShader != "" {
		vertex = r.compileShader(r.runtime.Device, mat.vertexShader, defines)
	}

	if mat.fragmentShader != "" {
		fragment = r.compileShader(r.runtime.Device, mat.fragmentShader, defines)
	}

	depthCompare := wgpu.CompareFunctionAlways
	if mat.depthTest {
		depthCompare = mat.depthFunc.ToWGPU()
	}

	depthWrite := wgpu.OptionalBoolFalse
	if mat.depthWrite && mat.depthTest {
		depthWrite = wgpu.OptionalBoolTrue
	}

	var fragmentState *wgpu.FragmentState
	if fragment != nil {
		var targets []wgpu.ColorTargetState
		if !shadow {
			writeMask := wgpu.ColorWriteMaskNone
			if mat.colorWrite {
				writeMask = wgpu.ColorWriteMaskAll
			}
			targets = []wgpu.ColorTargetState{{
				Format:    renderTarget.GetFormat(),
				Blend:     mat.blending.ToWGPU(),
				WriteMask: writeMask,
			}}
		}
		// When shadow && fragment != nil: depth-only pass with a fragment shader
		// (e.g. point shadow cube faces that write linear depth via @builtin(frag_depth)).
		// Use empty Targets so no color attachment is required.
		fragmentState = &wgpu.FragmentState{
			Module:     fragment,
			EntryPoint: "main",
			Targets:    targets,
		}
	}

	return r.runtime.Device.CreateRenderPipeline(wgpu.RenderPipelineDescriptor{
		Layout: layout,
		Vertex: wgpu.VertexState{
			Module:     vertex,
			EntryPoint: "main",
			Buffers:    vertexLayout,
		},
		Fragment: fragmentState,
		Primitive: wgpu.PrimitiveState{
			Topology:  wgpu.PrimitiveTopologyTriangleList,
			FrontFace: wgpu.FrontFaceCCW,
			CullMode:  mat.side.ToWGPU(),
		},
		DepthStencil: &wgpu.DepthStencilState{
			Format:            depthTarget.GetFormat(),
			DepthWriteEnabled: depthWrite,
			DepthCompare:      depthCompare,
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
	r.createPointShadowResources()
	r.createGlobalBindGroupLayouts()
	r.createGlobalBuffers()
	r.createGlobalBindGroups()
}

func (r *Renderer) createShadowResources() {
	r.shadowMap = &TextureData{
		format: wgpu.TextureFormatDepth32Float,
		sampler: Sampler{
			AddressModeU:  wgpu.AddressModeClampToEdge,
			AddressModeV:  wgpu.AddressModeClampToEdge,
			AddressModeW:  wgpu.AddressModeClampToEdge,
			MagFilter:     wgpu.FilterModeLinear,
			MinFilter:     wgpu.FilterModeLinear,
			MipmapFilter:  wgpu.MipmapFilterModeNearest,
			LodMaxClamp:   32,
			Compare:       wgpu.CompareFunctionLessEqual,
			MaxAnisotropy: 1,
		},
	}
	r.shadowMap.gpuRef = r.runtime.Device.CreateTexture(&wgpu.TextureDescriptor{
		Label:         "Shadow Map Array",
		Usage:         wgpu.TextureUsageRenderAttachment | wgpu.TextureUsageTextureBinding,
		Dimension:     wgpu.TextureDimension2D,
		Format:        wgpu.TextureFormatDepth32Float,
		MipLevelCount: 1,
		SampleCount:   1,
		Size: wgpu.Extent3D{
			Width:              DefaultShadowMapSize,
			Height:             DefaultShadowMapSize,
			DepthOrArrayLayers: MaxDirectionalLights + MaxSpotLights,
		},
	})
	r.shadowMap.gpuView = r.shadowMap.gpuRef.CreateView(&wgpu.TextureViewDescriptor{
		Format:          wgpu.TextureFormatDepth32Float,
		Dimension:       wgpu.TextureViewDimension2DArray,
		BaseMipLevel:    0,
		MipLevelCount:   1,
		BaseArrayLayer:  0,
		ArrayLayerCount: MaxDirectionalLights + MaxSpotLights,
		Aspect:          wgpu.TextureAspectDepthOnly,
	})
	r.shadowMap.gpuSampler = r.getOrCreateSampler(r.shadowMap.sampler)

	for i := range r.shadowLayerViews {
		r.shadowLayerViews[i] = r.shadowMap.gpuRef.CreateView(&wgpu.TextureViewDescriptor{
			Format:          wgpu.TextureFormatDepth32Float,
			Dimension:       wgpu.TextureViewDimension2D,
			BaseMipLevel:    0,
			MipLevelCount:   1,
			BaseArrayLayer:  uint32(i),
			ArrayLayerCount: 1,
			Aspect:          wgpu.TextureAspectDepthOnly,
		})
	}

	r.shadowMat = r.NewShadowMaterial()
}

func (r *Renderer) createPointShadowResources() {
	r.pointShadowMap = &TextureData{
		format: wgpu.TextureFormatDepth32Float,
		sampler: Sampler{
			AddressModeU:  wgpu.AddressModeClampToEdge,
			AddressModeV:  wgpu.AddressModeClampToEdge,
			AddressModeW:  wgpu.AddressModeClampToEdge,
			MagFilter:     wgpu.FilterModeLinear,
			MinFilter:     wgpu.FilterModeLinear,
			MipmapFilter:  wgpu.MipmapFilterModeNearest,
			LodMaxClamp:   32,
			Compare:       wgpu.CompareFunctionLessEqual,
			MaxAnisotropy: 1,
		},
	}
	r.pointShadowMap.gpuRef = r.runtime.Device.CreateTexture(&wgpu.TextureDescriptor{
		Label:         "Point Shadow Cube Array",
		Usage:         wgpu.TextureUsageRenderAttachment | wgpu.TextureUsageTextureBinding,
		Dimension:     wgpu.TextureDimension2D,
		Format:        wgpu.TextureFormatDepth32Float,
		MipLevelCount: 1,
		SampleCount:   1,
		Size: wgpu.Extent3D{
			Width:              DefaultShadowMapSize,
			Height:             DefaultShadowMapSize,
			DepthOrArrayLayers: MaxPointLights * 6,
		},
	})
	r.pointShadowMap.gpuView = r.pointShadowMap.gpuRef.CreateView(&wgpu.TextureViewDescriptor{
		Format:          wgpu.TextureFormatDepth32Float,
		Dimension:       wgpu.TextureViewDimensionCubeArray,
		BaseMipLevel:    0,
		MipLevelCount:   1,
		BaseArrayLayer:  0,
		ArrayLayerCount: MaxPointLights * 6,
		Aspect:          wgpu.TextureAspectDepthOnly,
	})
	r.pointShadowMap.gpuSampler = r.getOrCreateSampler(r.pointShadowMap.sampler)

	for i := range r.pointShadowLayerViews {
		r.pointShadowLayerViews[i] = r.pointShadowMap.gpuRef.CreateView(&wgpu.TextureViewDescriptor{
			Format:          wgpu.TextureFormatDepth32Float,
			Dimension:       wgpu.TextureViewDimension2D,
			BaseMipLevel:    0,
			MipLevelCount:   1,
			BaseArrayLayer:  uint32(i),
			ArrayLayerCount: 1,
			Aspect:          wgpu.TextureAspectDepthOnly,
		})
	}

	r.pointShadowMat = r.NewPointShadowMaterial()
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
			{
				Binding:    PointShadowMapBinding,
				Visibility: wgpu.ShaderStageFragment,
				Texture: wgpu.TextureBindingLayout{
					SampleType:    wgpu.TextureSampleTypeDepth,
					ViewDimension: wgpu.TextureViewDimensionCubeArray,
					Multisampled:  false,
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
				TextureView: r.shadowMap.gpuView,
			},
			{
				Binding: ShadowSamplerBinding,
				Sampler: r.shadowMap.gpuSampler,
			},
			{
				Binding:     PointShadowMapBinding,
				TextureView: r.pointShadowMap.gpuView,
			},
		},
	})
}

// appendInstances adds the InstanceUniform entries for a drawing to the slice.
// For regular meshes (instMatrices == nil) it appends one entry; for instanced
// meshes it appends one entry per instance (world = instWorldTransform × localMat).
func appendInstances(instances InstancesUniform, d *drawing) InstancesUniform {
	if d.instMatrices == nil {
		return append(instances, InstanceUniform{d.model, d.modelInv})
	}
	for _, localMat := range d.instMatrices {
		world := d.instWorldTransform.Mul4x4(localMat)
		worldInv := world.Inv()
		instances = append(instances, InstanceUniform{world, worldInv})
	}
	return instances
}

// collectRenderList populates list by iterating the scene's compact payload tables
// directly, avoiding a full tree traversal. No frustum culling is applied here;
// call cull separately for each frustum.
func (r *Renderer) collectRenderList(list *renderList, scene *Scene) {
	for _, md := range scene.meshes {
		flags := scene.GetFlags(md.ownerNode)
		if !flags.IsAlive() || !flags.IsVisible() {
			continue
		}

		model := scene.GetWorldTransform(md.ownerNode)
		localCenter := md.boundingSphere.Center
		worldCenter := model.Mul4x1(glm.Vec4f{localCenter[0], localCenter[1], localCenter[2], 1})
		d := drawing{
			instanceCount: 1,
			geo:           r.geometries.get(md.geometry.ref.ID()),
			mat:           r.materials.get(md.material.ref.ID()),
			model:         model,
			modelInv:      scene.GetWorldTransformInv(md.ownerNode),
			bounds: Sphere{
				Center: glm.Vec3f{worldCenter[0], worldCenter[1], worldCenter[2]},
				Radius: md.boundingSphere.Radius,
			},
		}

		if flags.CastShadow() {
			list.shadowCasters = append(list.shadowCasters, d)
		}
		list.visible = append(list.visible, d)
	}

	for _, imd := range scene.instancedMeshes {
		flags := scene.GetFlags(imd.ownerNode)
		if !flags.IsAlive() || !flags.IsVisible() || len(imd.matrices) == 0 {
			continue
		}

		geo := r.geometries.get(imd.geometry.ref.ID())
		worldTransform := scene.GetWorldTransform(imd.ownerNode)
		geoBounds := geo.BoundingSphere()

		// Compute a conservative world-space bounding sphere over all instances.
		var sumPos glm.Vec3f
		for _, m := range imd.matrices {
			wm := worldTransform.Mul4x4(m)
			sumPos = sumPos.Add(glm.Vec3f{wm[12], wm[13], wm[14]})
		}
		center := sumPos.Scale(1.0 / float32(len(imd.matrices)))
		var maxDist float32
		for _, m := range imd.matrices {
			wm := worldTransform.Mul4x4(m)
			dist := center.Sub(glm.Vec3f{wm[12], wm[13], wm[14]}).Length()
			if dist > maxDist {
				maxDist = dist
			}
		}

		d := drawing{
			instanceCount:      uint32(len(imd.matrices)),
			geo:                geo,
			mat:                r.materials.get(imd.material.ref.ID()),
			bounds:             Sphere{Center: center, Radius: maxDist + geoBounds.Radius},
			instMatrices:       imd.matrices,
			instWorldTransform: worldTransform,
		}

		if flags.CastShadow() {
			list.shadowCasters = append(list.shadowCasters, d)
		}
		list.visible = append(list.visible, d)
	}

	for i := range scene.dirLights {
		ld := scene.dirLights[i]
		if scene.flags[ld.ownerNode]&flagAlive == 0 {
			continue
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

	for i := range scene.spotLights {
		ld := scene.spotLights[i]
		if scene.flags[ld.ownerNode]&flagAlive == 0 {
			continue
		}
		list.spotLights = append(list.spotLights, ld)
	}

	for i := range scene.pointLights {
		ld := scene.pointLights[i]
		if scene.flags[ld.ownerNode]&flagAlive == 0 {
			continue
		}
		list.pointLights = append(list.pointLights, ld)
	}
}

// cull appends drawings from src into dst, keeping only those whose bounds
// intersect frustum. Pass dst[:0] to compact in-place or a separate slice to
// preserve src for reuse across multiple lights.
func cull(frustum Frustum, src []drawing, dst []drawing) []drawing {
	for _, d := range src {
		if frustum.ContainsSphere(d.bounds) {
			dst = append(dst, d)
		}
	}
	return dst
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
