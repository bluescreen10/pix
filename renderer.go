package pix

import (
	"fmt"
	"log/slog"
	"math"
	"math/bits"
	"os"
	"sync"
	"time"
	"unsafe"

	"github.com/bluescreen10/dawn-go/wgpu"
	"github.com/bluescreen10/pix/glm"
	"github.com/bluescreen10/pix/gpu"
	"github.com/bluescreen10/pix/internal/mem"
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
	SkeletonSet // only present in skinned-mesh pipelines
)

// GeometrySet holds the vertex-pulling attribute streams. It occupies the same
// slot as SkeletonSet because a pipeline is either pulling (non-skinned) or
// skinning (fetch), never both.
const GeometrySet = SkeletonSet

// Vertex shaders that pull geometry from the GeometrySystem SSBOs (no vertex
// buffers, drawable resolved through the culler's visible buffer). Directional
// and spot shadows pull position only; point shadows stay on vertex fetch.
const (
	geometryVertexShader    = "vertex.wesl"
	shadowVertexShader      = "shadow_vertex.wgsl"
	pointShadowVertexShader = "point_shadow_vertex.wgsl"
)

// Global Bindings
const (
	CameraBinding = iota
	LightsBinding
	ShadowMapBinding
	ShadowSamplerBinding
	PointShadowMapBinding
	EnvironmentMapBinding
	EnvironmentMapSamplerBinding
)

// Instance Bindings
const (
	ModelsBinding = iota
	DrawablesBinding
)

// Pipeline type indices for per-mesh pipeline cache.
const (
	PipelineGeometry    = 0
	PipelineShadow      = 1
	PipelinePointShadow = 2
	numPipelineTypes    = 3
)

var renderListPool = sync.Pool{
	New: func() any {
		return &renderList{}
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
	materials mem.Slab[MaterialData]
	textures  mem.Slab[TextureData]
	skeletons mem.Slab[SkeletonData]

	samplerCache  map[Sampler]*wgpu.Sampler
	defaultTexRef Ref[Texture]
	deferredFree  []deferredFreeEntry

	gpu           *gpu.Instance
	width, height uint32
	frameCount    uint32
	logger        *slog.Logger

	pipelineCache *pipelineCache

	// geometry attribute storage for GPU-driven vertex pulling.
	geometries *GeometrySystem

	// GPU frustum culling + indirect drawing for the main pass.
	culler *gpuCuller

	// Optional GPU timing (timestamp queries); nil if unsupported.
	gpuTimer *gpuTimer

	// global
	sceneStates map[uint32]*sceneRenderState
	//cameraUniformBuffer *wgpu.Buffer
	//lightsUniformBuffer   *wgpu.Buffer
	globalBindGroupLayout *wgpu.BindGroupLayout
	//globalBindGroup       *wgpu.BindGroup

	// Stable per-node model buffer — slot = scene node index. The normal matrix
	// is derived from model in-shader, so no inverse-model buffer is kept.
	modelBuf       gpu.Buffer
	objectsBindGrp *wgpu.BindGroup
	objectsCap     uint32

	// Flattened drawable table, mirroring scene.drawables. Uploaded verbatim and
	// bound in the instance set; the vertex shaders index it by drawable id to
	// resolve the transform.
	drawableBuf  gpu.Buffer
	drawablesCap uint32

	// objectsBindGrp is rebuilt whenever any of its backing buffers (model,
	// drawables) is reallocated, tracked by this flag.
	objectsBindGrpDirty bool

	// Layout for the instance set (binding 0 = objects storage buffer).
	// Used by both regular and instanced mesh draws; the bind group differs.
	instanceStorageBindGroupLayout *wgpu.BindGroupLayout

	skeletonBGL *wgpu.BindGroupLayout

	// shadow (directional + spot) — 2D depth array, one layer per shadow
	shadowArray *TextureArray
	shadowMat   *ShadowMaterial

	// shadow (point — cube array) — 6 consecutive layers per point light
	pointShadowArray *TextureArray
	pointShadowMat   *PointShadowMaterial

	// depth buffer
	depthTexture     *wgpu.Texture
	depthTextureView *wgpu.TextureView

	// enviroment material
	environmentMaterial *EquirectMaterial

	// debug text overlay
	debugText      *debugTextRenderer
	DebugTexts     []DebugText
	DebugTextColor glm.Color4f

	Stats     *RendererStats
	showStats bool
	shaders   *wesl.Compiler
}

func NewRenderer(width, height uint32) *Renderer {
	return &Renderer{
		width:          width,
		height:         height,
		logger:         slog.New(slog.NewTextHandler(os.Stderr, nil)),
		gpu:            &gpu.Instance{},
		Stats:          NewRendererStats(60),
		pipelineCache:  newPipelineCache(),
		shaders:        wesl.New(),
		DebugTextColor: glm.Color4f{1, 1, 1, 1},
		sceneStates:    map[uint32]*sceneRenderState{},
	}
}

func (r *Renderer) Init(descriptor wgpu.SurfaceDescriptor) error {
	if err := r.gpu.Init(r.width, r.height, descriptor); err != nil {
		slog.Error("error creating gpu", slog.Any("err", err))
		return err
	}

	r.initResources()
	if err := r.shaders.ParseFS(shaderlib); err != nil {
		panic(err)
	}
	r.createGlobalResources()
	r.initDebugText()
	return nil
}

func (r *Renderer) Destroy() {
	if r.debugText != nil {
		r.debugText.destroy()
		r.debugText = nil
	}

	if r.gpuTimer != nil {
		r.gpuTimer.Destroy()
		r.gpuTimer = nil
	}

	if r.culler != nil {
		r.culler.Destroy()
		r.culler = nil
	}

	if r.geometries != nil {
		r.geometries.Destroy()
		r.geometries = nil
	}

	r.gpu.Destroy()
	r.gpu = nil

	if r.skeletonBGL != nil {
		r.skeletonBGL.Release()
		r.skeletonBGL = nil
	}

	if r.objectsBindGrp != nil {
		r.objectsBindGrp.Release()
		r.objectsBindGrp = nil
	}

	for _, state := range r.sceneStates {
		state.bindGroup.Release()
		state.bindGroup = nil
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

	if r.shadowArray != nil {
		r.shadowArray.Destroy()
		r.shadowArray = nil
	}

	if r.pointShadowArray != nil {
		r.pointShadowArray.Destroy()
		r.pointShadowArray = nil
	}

	r.destroyResources()
}

// ensureObjectsCap grows the model buffer to hold at least `need` entries.
func (r *Renderer) ensureObjectsCap(need uint32) {
	if r.modelBuf.Valid() && r.objectsCap >= need {
		return
	}
	if r.modelBuf.Valid() {
		r.gpu.ReleaseBuffer(r.modelBuf)
	}
	if r.objectsCap == 0 {
		r.objectsCap = InitialStorageCapacity
	}
	for r.objectsCap < need {
		r.objectsCap *= 2
	}
	matSize := uint64(r.objectsCap) * uint64(unsafe.Sizeof(glm.Mat4f{}))
	r.modelBuf = r.gpu.CreateBuffer(gpu.BufferDescriptor{
		Label: "Model buffer",
		Size:  matSize,
		Usage: gpu.BufferUsageStorage | gpu.BufferUsageCopyDst,
	})
	r.objectsBindGrpDirty = true
	if r.culler != nil {
		r.culler.dirty = true // batch/cull bind groups reference modelBuf
	}
}

// ensureDrawablesCap grows the drawables buffer to hold at least `need` entries.
func (r *Renderer) ensureDrawablesCap(need uint32) {
	if r.drawableBuf.Valid() && r.drawablesCap >= need {
		return
	}
	if r.drawableBuf.Valid() {
		r.gpu.ReleaseBuffer(r.drawableBuf)
	}
	if r.drawablesCap == 0 {
		r.drawablesCap = InitialStorageCapacity
	}
	for r.drawablesCap < need {
		r.drawablesCap *= 2
	}
	size := uint64(r.drawablesCap) * uint64(unsafe.Sizeof(drawable{}))
	r.drawableBuf = r.gpu.CreateBuffer(gpu.BufferDescriptor{
		Label: "Drawables buffer",
		Size:  size,
		Usage: gpu.BufferUsageStorage | gpu.BufferUsageCopyDst,
	})
	r.objectsBindGrpDirty = true
	if r.culler != nil {
		r.culler.dirty = true // batch/cull bind groups reference drawableBuf
	}
}

// ensureObjectsBindGroup rebuilds the shared instance-set bind group when any of
// its backing buffers has been reallocated.
func (r *Renderer) ensureObjectsBindGroup() {
	if !r.objectsBindGrpDirty && r.objectsBindGrp != nil {
		return
	}
	if r.objectsBindGrp != nil {
		r.objectsBindGrp.Release()
	}
	r.objectsBindGrp = r.gpu.CreateBindGroup(wgpu.BindGroupDescriptor{
		Label:  "Objects bind group",
		Layout: r.instanceStorageBindGroupLayout,
		Entries: []wgpu.BindGroupEntry{
			{Binding: ModelsBinding, Buffer: r.gpu.RawBuffer(r.modelBuf), Offset: 0, Size: wgpu.WholeSize},
			{Binding: DrawablesBinding, Buffer: r.gpu.RawBuffer(r.drawableBuf), Offset: 0, Size: wgpu.WholeSize},
		},
	})
	r.objectsBindGrpDirty = false
}

func (r *Renderer) ensureDepthTextureSize(width, height uint32) {
	if r.depthTexture != nil && r.depthTexture.GetWidth() == width && r.depthTexture.GetHeight() == r.height {
		return
	}

	if r.depthTexture != nil {
		r.depthTextureView.Release()
		r.depthTexture.Destroy()
	}

	r.depthTexture = r.gpu.CreateTexture(&wgpu.TextureDescriptor{
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
	visible           []drawable
	shadowCasters     []drawable
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
	r.Stats.StartFrame()
	defer r.Stats.EndFrame()
	// Deliver async callbacks (e.g. GPU-timing buffer maps) completed since the
	// last frame, so the stats below reflect them.
	r.gpu.ProcessEvents()
	if r.showStats {
		fps := r.Stats.FPS()
		color := glm.Color4f{0, 1, 0, 1}
		if fps < 30 {
			color = glm.Color4f{1, 0, 0, 1}
		}
		r.DebugPrint(fmt.Sprintf("FPS %.1f", fps), glm.Vec2f{10, 10}, 32, color)
		r.DebugPrint(fmt.Sprintf("CPU %.2f ms", float64(r.Stats.AvgCPUTime().Microseconds())/1000), glm.Vec2f{10, 44}, 32, color)
		if r.gpuTimer != nil {
			r.DebugPrint(fmt.Sprintf("GPU %.2f ms", float64(r.Stats.AvgGPUTime().Microseconds())/1000), glm.Vec2f{10, 78}, 32, color)
		}
	}

	r.acquireNextFrame(&ctx)
	// CPU rendering cost: list building, culling, encoding and submit — measured
	// after the (possibly blocking) acquire and stopped before the vsync present.
	cpuStart := time.Now()

	transformsDirty := scene.UpdateTransforms()
	scene.UpdateVisibility()

	drawablesDirty := scene.UpdateDrawables()
	if drawablesDirty {
		// Assigns each drawable its (material, geometry) batchID and lays out the
		// per-batch visible regions; must run before drawables are uploaded.
		r.culler.rebuildBatches(scene)
	}

	list := renderListPool.Get().(*renderList)
	defer list.release()

	viewProjection := camera.ViewProjection()
	frustum := NewFrustumFromViewProjection(viewProjection)
	r.collectRenderList(list, scene)

	if transformsDirty {
		r.syncMeshInstances(scene)
	}
	r.syncDrawables(scene, drawablesDirty)
	r.syncSkeletons(scene)
	r.geometries.sync()
	r.ensureObjectsBindGroup()
	r.culler.prep()

	list.visible = cull(frustum, list.visible, list.visible[:0])

	var useLights bool

	validVisible := 0
	for i := range list.visible {
		d := &list.visible[i]
		mat := r.materials.Get(d.materialID)
		if err := prepareMaterial(r.gpu.Device(), mat, r); err != nil {
			r.logger.Error("error preparing material", slog.Any("err", err))
			continue
		}
		if mat.isLit {
			useLights = true
		}
		list.visible[validVisible] = *d
		validVisible++
	}
	list.visible = list.visible[:validVisible]

	sceneState := r.getSceneState(scene)

	cameraUniform := CameraUniform{
		viewProj:    viewProjection,
		invViewProj: viewProjection.Mat3().Mat4().Inv(),
		position:    camera.Position().Vec4(),
	}
	r.gpu.WriteBuffer(sceneState.cameraUniform, 0, cameraUniform.Bytes())

	ctx.encoder = r.gpu.CreateCommandEncoder(nil)

	// GPU cull views: view 0 is the camera; each dir/spot shadow gets the next.
	drawableCount := uint32(len(scene.drawables))
	shadowView := mainView + 1

	if useLights {
		var lightsUniform LightsUniform

		count := min(MaxDirectionalLights, len(list.directionalLights))
		lightsUniform.DirectionalLightCount = uint32(count)
		for i := range list.directionalLights[:count] {
			ld := &list.directionalLights[i]
			var lightSpaceMat glm.Mat4f
			if vp, ok := ld.shadowVP(scene); ok {
				if ld.shadow.layerIndex < 0 {
					if base, ok := r.shadowArray.AllocLayers(1); ok {
						ld.shadow.layerIndex = int(base)
					} else {
						r.logger.Warn("shadow array full, directional light shadow skipped")
					}
				}
				if ld.shadow.layerIndex >= 0 {
					lightSpaceMat = vp
					viewIdx := shadowView
					shadowView++
					r.culler.cullView(ctx.encoder, viewIdx, NewFrustumFromViewProjection(vp), true, drawableCount)
					ctx.depthTarget = r.shadowArray.Texture()
					ctx.depthTargetView = r.shadowArray.LayerView(uint32(ld.shadow.layerIndex))
					r.renderShadowMap(&ctx, vp, viewIdx)
				}
			}
			lightsUniform.DirectionalLights[i] = ld.toUniform(scene, lightSpaceMat)
		}

		if list.ambientLight != nil {
			lightsUniform.AmbientLight = AmbientLightUniform{
				color:     list.ambientLight.color.RGBA(),
				intensity: list.ambientLight.intensity,
			}
		}

		spotCount := min(MaxSpotLights, len(list.spotLights))
		lightsUniform.SpotLightCount = uint32(spotCount)
		for i := range list.spotLights[:spotCount] {
			ld := &list.spotLights[i]
			var lightSpaceMat glm.Mat4f
			if vp, ok := ld.shadowVP(scene); ok {
				if ld.shadow.layerIndex < 0 {
					if base, ok := r.shadowArray.AllocLayers(1); ok {
						ld.shadow.layerIndex = int(base)
					} else {
						r.logger.Warn("shadow array full, spot light shadow skipped")
					}
				}
				if ld.shadow.layerIndex >= 0 {
					lightSpaceMat = vp
					viewIdx := shadowView
					shadowView++
					r.culler.cullView(ctx.encoder, viewIdx, NewFrustumFromViewProjection(vp), true, drawableCount)
					ctx.depthTarget = r.shadowArray.Texture()
					ctx.depthTargetView = r.shadowArray.LayerView(uint32(ld.shadow.layerIndex))
					r.renderShadowMap(&ctx, vp, viewIdx)
				}
			}
			lightsUniform.SpotLights[i] = ld.toUniform(scene, lightSpaceMat)
		}

		pointCount := min(MaxPointLights, len(list.pointLights))
		lightsUniform.PointLightCount = uint32(pointCount)
		for i := range list.pointLights[:pointCount] {
			ld := &list.pointLights[i]
			if ld.shadow != nil {
				if ld.shadow.layerIndex < 0 {
					if base, ok := r.pointShadowArray.AllocLayers(6); ok {
						ld.shadow.layerIndex = int(base)
					} else {
						r.logger.Warn("point shadow array full, point light shadow skipped")
					}
				}
				if ld.shadow.layerIndex >= 0 {
					r.renderPointShadowCube(&ctx, nodeWorldPos(scene, ld.ownerNode), ld.shadow, drawableCount)
				}
			}
			lightsUniform.PointLights[i] = ld.toUniform(scene)
		}

		r.gpu.WriteBuffer(sceneState.lightsUniform, 0, lightsUniform.Bytes())
	}

	// All shadow passes have been submitted. Create the main encoder now so the
	// shadow texture transitions from RenderAttachment to TextureBinding are complete.

	r.ensureDepthTextureSize(ctx.texture.GetWidth(), ctx.texture.GetHeight())
	ctx.depthTarget = r.depthTexture
	ctx.depthTargetView = r.depthTextureView

	// GPU frustum cull → per-batch indirect args + compacted visible buffer.
	// Encoded before the main pass on the same encoder, so its writes are visible.
	r.culler.cullView(ctx.encoder, mainView, frustum, false, uint32(len(scene.drawables)))

	renderPass := r.beginRendering(&ctx, scene.background)
	renderPass.SetBindGroup(GlobalSet, sceneState.bindGroup, nil)
	// The geometry set (attribute streams) is shared by all pull draws. It sits at
	// the same slot as the skeleton set, which pull pipelines don't use.
	renderPass.SetBindGroup(GeometrySet, r.geometries.BindGroup(), nil)

	// Main pass: one indirect draw per (material, geometry) batch; the cull pass
	// filled each batch's instance count and compacted visible list. The instance
	// set (models, drawables, visible@region) is bound per batch inside draw.
	r.culler.drawMain(renderPass, ctx.texture, ctx.depthTarget)

	// has an env map — drawn as a procedural fullscreen triangle (no geometry).
	if scene.backgroundMap.Valid() {
		mat := r.environmentMaterial.data()
		if err := prepareMaterial(r.gpu.Device(), mat, r); err != nil {
			r.logger.Error("error preparing equirect material", slog.Any("err", err))
		} else {
			pipeline := r.getPipeline(mat, nil, ctx.texture, ctx.depthTarget)
			renderPass.SetPipeline(pipeline)
			renderPass.SetBindGroup(MaterialSet, mat.gpuBindGroup, nil)
			renderPass.SetBindGroup(InstanceSet, r.objectsBindGrp, nil)
			renderPass.Draw(3, 1, 0, 0)
		}
	}

	r.endRendering(&ctx, renderPass)

	if r.gpuTimer != nil {
		r.gpuTimer.readback() // map the just-submitted timestamps (async)
	}

	if r.debugText != nil && len(r.DebugTexts) > 0 {
		r.debugText.render(r, &ctx, r.DebugTexts, r.DebugTextColor)
	}

	r.Stats.AddCPUTime(time.Since(cpuStart))
	r.presentFrame(&ctx)
	r.debugClear()
	r.drainDeferredFree()
}

// DebugPrint adds a persistent text overlay at pixel position pos. Entries accumulate
// across frames until DebugClear is called.
func (r *Renderer) DebugPrint(text string, pos glm.Vec2f, size float32, color glm.Color4f) {
	r.DebugTexts = append(r.DebugTexts, DebugText{Text: text, X: pos[0], Y: pos[1], Size: size, Color: color})
}

// DebugClear removes all debug text entries.
func (r *Renderer) debugClear() {
	r.DebugTexts = r.DebugTexts[:0]
}

// renderShadowMap renders a directional/spot shadow map via GPU cull view viewIdx
// (already dispatched on ctx.encoder) using indirect, position-only pull draws.
func (r *Renderer) renderShadowMap(ctx *renderContext, vp glm.Mat4f, viewIdx int) {
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
	defer func() { pass.End(); pass.Release() }()

	r.shadowMat.SetViewProjection(vp)
	if err := prepareMaterial(r.gpu.Device(), r.shadowMat.data(), r); err != nil {
		r.logger.Error("error preparing shadow material", slog.Any("err", err))
		return
	}

	pass.SetBindGroup(0, r.shadowMat.BindGroup(), nil)
	pass.SetBindGroup(2, r.geometries.BindGroup(), nil) // geometry set (pull)
	r.culler.drawShadow(pass, viewIdx, r.shadowMat.data(), ctx.depthTarget)
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

// renderPointShadowCube renders the 6 cube faces of a point light's shadow map.
// Each face is an independent GPU-cull + indirect, position-only pull pass on its
// own encoder: the per-face view-projection is written to the shared point-shadow
// material uniform, so the faces must not share an encoder (queue writes to one
// uniform buffer would alias). The single reused cull view (pointShadowView) is
// safe for the same reason — each face is fully submitted before the next.
func (r *Renderer) renderPointShadowCube(ctx *renderContext, lightPos glm.Vec3f, shadow *PointShadow, drawableCount uint32) {
	near := float32(0.1)
	proj := glm.PerspectiveRH(float32(math.Pi/2), float32(1.0), near, shadow.far)
	// Negate h (proj[5]) so rendered V matches WebGPU cube map sampling convention (tc = -ry).
	// Without this flip the cube faces are upside-down relative to textureSampleCompare.
	proj[5] = -proj[5]

	ctx.depthTarget = r.pointShadowArray.Texture()

	for face, cf := range cubeFaces {
		view := glm.LookAtRH(lightPos, lightPos.Add(cf.fwd), cf.up)
		vp := proj.Mul4x4(view)

		r.pointShadowMat.SetFaceUniforms(vp, lightPos, shadow.far)
		if err := prepareMaterial(r.gpu.Device(), r.pointShadowMat.data(), r); err != nil {
			r.logger.Error("error preparing point shadow material", slog.Any("err", err))
			continue
		}

		ctx.depthTargetView = r.pointShadowArray.LayerView(uint32(shadow.layerIndex) + uint32(face))

		encoder := r.gpu.CreateCommandEncoder(nil)
		// Cull this face's frustum on the same encoder so its indirect args are
		// written before the render pass reads them.
		r.culler.cullView(encoder, pointShadowView, NewFrustumFromViewProjection(vp), true, drawableCount)

		pass := encoder.BeginRenderPass(wgpu.RenderPassDescriptor{
			DepthStencilAttachment: &wgpu.RenderPassDepthStencilAttachment{
				View:            ctx.depthTargetView,
				DepthLoadOp:     wgpu.LoadOpClear,
				DepthStoreOp:    wgpu.StoreOpStore,
				DepthClearValue: 1.0,
			},
		})
		pass.SetBindGroup(0, r.pointShadowMat.BindGroup(), nil)
		pass.SetBindGroup(2, r.geometries.BindGroup(), nil) // geometry set (pull)
		r.culler.drawShadow(pass, pointShadowView, r.pointShadowMat.data(), ctx.depthTarget)
		pass.End()
		pass.Release()

		cmd := encoder.Finish(nil)
		r.gpu.Queue().Submit(cmd)
		cmd.Release()
		encoder.Release()
	}
}

func primitiveTopologyFor(mat *MaterialData) wgpu.PrimitiveTopology {
	if mat.flags&WireframeFlag != 0 {
		return wgpu.PrimitiveTopologyLineList
	}
	return wgpu.PrimitiveTopologyTriangleList
}

func (r *Renderer) acquireNextFrame(ctx *renderContext) {
	ctx.texture = r.gpu.Surface().GetCurrentTexture()
	ctx.view = ctx.texture.CreateView(nil)
	// Main encoder is created after all shadow passes are submitted,
	// so the shadow texture is fully written before it is bound as a sampler input.
}

func (r *Renderer) presentFrame(ctx *renderContext) {
	r.gpu.Surface().Present()
	ctx.view.Release()
	ctx.view = nil

	ctx.texture.Destroy()
	ctx.texture = nil
}

func (r *Renderer) beginRendering(ctx *renderContext, bgColor glm.Color4f) *wgpu.RenderPassEncoder {
	desc := wgpu.RenderPassDescriptor{
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
	}
	if r.gpuTimer != nil {
		desc.TimestampWrites = r.gpuTimer.passWrites()
	}
	return ctx.encoder.BeginRenderPass(desc)
}

func (r *Renderer) endRendering(ctx *renderContext, pass *wgpu.RenderPassEncoder) {
	pass.End()
	pass.Release()

	if r.gpuTimer != nil {
		r.gpuTimer.end(ctx.encoder)
	}

	cmdBuf := ctx.encoder.Finish(nil)
	r.gpu.Queue().Submit(cmdBuf)

	cmdBuf.Release()
	ctx.encoder.Release()
	ctx.encoder = nil
}

func (r *Renderer) getPipeline(mat *MaterialData, geo *geometryEntry, renderTarget, depthTarget *wgpu.Texture) *wgpu.RenderPipeline {
	shadow := renderTarget == nil

	var geoFlags GeometryFlags
	if geo != nil {
		geoFlags = geo.flags
	}
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
	p := r.createPipeline(mat, geo, renderTarget, depthTarget)
	r.pipelineCache.SetRenderPipeline(key, p)
	return p
}

// createPipeline builds a render pipeline. When renderTarget is nil the
// pipeline is depth-only (no fragment stage).
func (r *Renderer) createPipeline(mat *MaterialData, geo *geometryEntry, renderTarget, depthTarget *wgpu.Texture) *wgpu.RenderPipeline {
	shadow := renderTarget == nil

	// Pull pipelines (main pass + all shadows) resolve the drawable through the
	// culler's compacted visible buffer and pull geometry from the SSBOs, so they
	// use the culler's instance layout + the geometry set. Non-pull pipelines (the
	// env map) draw procedurally and bind neither. Nothing uses vertex buffers.
	pull := mat.usesPull()
	instanceLayout := r.instanceStorageBindGroupLayout
	if pull {
		instanceLayout = r.culler.instanceLayout
	}

	var bindGroupLayouts []*wgpu.BindGroupLayout
	if shadow {
		bindGroupLayouts = []*wgpu.BindGroupLayout{mat.gpuBindGroupLayout, instanceLayout}
	} else {
		bindGroupLayouts = []*wgpu.BindGroupLayout{r.globalBindGroupLayout, mat.gpuBindGroupLayout, instanceLayout}
	}
	if pull {
		bindGroupLayouts = append(bindGroupLayouts, r.geometries.BindGroupLayout())
	}

	var geoFlags GeometryFlags
	if geo != nil {
		geoFlags = geo.flags
	}
	if shadow {
		geoFlags &= ShadowGeometryMask
	}

	layout := r.gpu.CreatePipelineLayout(wgpu.PipelineLayoutDescriptor{
		BindGroupLayouts: bindGroupLayouts,
	})
	defer layout.Release()

	defines := buildDefines(mat.flags, geoFlags)

	var vertex, fragment *wgpu.ShaderModule

	if mat.vertexShader != "" {
		vertex = r.compileShader(r.gpu.Device(), mat.vertexShader, defines)
	}

	if mat.fragmentShader != "" {
		fragment = r.compileShader(r.gpu.Device(), mat.fragmentShader, defines)
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

	return r.gpu.CreateRenderPipeline(wgpu.RenderPipelineDescriptor{
		Layout: layout,
		Vertex: wgpu.VertexState{
			Module:     vertex,
			EntryPoint: "main",
		},
		Fragment: fragmentState,
		Primitive: wgpu.PrimitiveState{
			Topology:  primitiveTopologyFor(mat),
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
	r.createEnvMapResources()
	r.createGlobalBindGroupLayouts()
	r.createGlobalBuffers()
	r.culler = newGPUCuller(r) // after shaders are parsed; compiles cull.wesl
	if r.gpu.HasTimestampQuery() {
		r.gpuTimer = newGPUTimer(r)
	}
	//r.createGlobalBindGroups()
}

func (r *Renderer) createEnvMapResources() {
	r.environmentMaterial = r.NewEquirectMaterial()
}

func (r *Renderer) createShadowResources() {
	r.shadowArray = newDepthTextureArray(
		r.gpu.Device(),
		DefaultShadowMapSize,
		MaxDirectionalLights+MaxSpotLights,
		wgpu.TextureViewDimension2DArray,
	)
	r.shadowMat = r.NewShadowMaterial()
}

func (r *Renderer) createPointShadowResources() {
	r.pointShadowArray = newDepthTextureArray(
		r.gpu.Device(),
		DefaultShadowMapSize,
		MaxPointLights*6,
		wgpu.TextureViewDimensionCubeArray,
	)
	r.pointShadowMat = r.NewPointShadowMaterial()
}

func (r *Renderer) createGlobalBindGroupLayouts() {
	r.globalBindGroupLayout = r.gpu.CreateBindGroupLayout(wgpu.BindGroupLayoutDescriptor{
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
			{
				Binding:    EnvironmentMapBinding,
				Visibility: wgpu.ShaderStageFragment,
				Texture: wgpu.TextureBindingLayout{
					SampleType:    wgpu.TextureSampleTypeFloat,
					ViewDimension: wgpu.TextureViewDimension2D,
					Multisampled:  false,
				},
			},
			{
				Binding:    EnvironmentMapSamplerBinding,
				Visibility: wgpu.ShaderStageFragment,
				Sampler: wgpu.SamplerBindingLayout{
					Type: wgpu.SamplerBindingTypeFiltering,
				},
			},
		},
	})

	r.skeletonBGL = r.gpu.CreateBindGroupLayout(wgpu.BindGroupLayoutDescriptor{
		Label: "Skeleton Bind Group Layout",
		Entries: []wgpu.BindGroupLayoutEntry{{
			Binding:    0,
			Visibility: wgpu.ShaderStageVertex,
			Buffer: wgpu.BufferBindingLayout{
				Type:             wgpu.BufferBindingTypeReadOnlyStorage,
				HasDynamicOffset: false,
				MinBindingSize:   uint64(unsafe.Sizeof(glm.Mat4f{})),
			},
		}},
	})

	matSize := uint64(unsafe.Sizeof(glm.Mat4f{}))
	r.instanceStorageBindGroupLayout = r.gpu.CreateBindGroupLayout(wgpu.BindGroupLayoutDescriptor{
		Label: "Instance/Model Bind Group Layout",
		Entries: []wgpu.BindGroupLayoutEntry{
			{
				Binding:    ModelsBinding,
				Visibility: wgpu.ShaderStageVertex | wgpu.ShaderStageFragment,
				Buffer: wgpu.BufferBindingLayout{
					Type:             wgpu.BufferBindingTypeReadOnlyStorage,
					HasDynamicOffset: false,
					MinBindingSize:   matSize,
				},
			},
			{
				Binding:    DrawablesBinding,
				Visibility: wgpu.ShaderStageVertex,
				Buffer: wgpu.BufferBindingLayout{
					Type:             wgpu.BufferBindingTypeReadOnlyStorage,
					HasDynamicOffset: false,
					MinBindingSize:   uint64(unsafe.Sizeof(drawable{})),
				},
			},
		},
	})
}

// syncMeshInstances uploads the world matrices for all scene nodes into the
// shared model buffer. Called only when scene transforms are dirty. worldInv is
// kept CPU-side (used for skinning) but no longer uploaded.
func (r *Renderer) syncMeshInstances(scene *Scene) {
	need := uint32(len(scene.flags))
	r.ensureObjectsCap(need)
	r.gpu.WriteBuffer(r.modelBuf, 0, wgpu.ToBytes(scene.world[:need]))
}

// syncDrawables grows the drawables buffer as needed and, when the table changed,
// re-uploads it verbatim. The rows hold local bounds, so they're structural and
// only change on add/remove; the buffer is indexed by drawable id (the draw
// call's instance id) to resolve each instance's transform.
func (r *Renderer) syncDrawables(scene *Scene, dirty bool) {
	n := uint32(len(scene.drawables))
	r.ensureDrawablesCap(n)
	if dirty && n > 0 {
		r.gpu.WriteBuffer(r.drawableBuf, 0, wgpu.ToBytes(scene.drawables))
	}
}

// syncSkeletons recomputes and uploads bone matrices for all skeletons referenced
// by the scene's skinned meshes.
func (r *Renderer) syncSkeletons(scene *Scene) {

	for _, smd := range scene.skinnedMeshes {
		if !smd.skeleton.Valid() {
			continue
		}

		id := smd.skeleton.ref.ID()
		sd := r.skeletons.Get(id)

		if sd.version == sd.gpuVersion {
			continue
		}

		sd.update(scene.worldInv[smd.ownerNode])

		needed := uint64(len(sd.boneMatrices)) * uint64(unsafe.Sizeof(glm.Mat4f{}))
		if !sd.gpuBuf.Valid() || r.gpu.BufferSize(sd.gpuBuf) < needed {
			sd.Destroy()
			r.gpu.ReleaseBuffer(sd.gpuBuf)
			buf := r.gpu.CreateBuffer(gpu.BufferDescriptor{
				Label: "Skeleton bone buffer",
				Size:  needed,
				Usage: gpu.BufferUsageStorage | gpu.BufferUsageCopyDst,
			})
			bg := r.gpu.CreateBindGroup(wgpu.BindGroupDescriptor{
				Label:  "Skeleton bind group",
				Layout: r.skeletonBGL,
				Entries: []wgpu.BindGroupEntry{{
					Binding: 0, Buffer: r.gpu.RawBuffer(buf), Offset: 0, Size: wgpu.WholeSize,
				}},
			})
			sd.gpuBuf = buf
			sd.bindGroup = bg
		}
		r.gpu.WriteBuffer(sd.gpuBuf, 0, wgpu.ToBytes(sd.boneMatrices))
	}
}

func (r *Renderer) createGlobalBuffers() {
	r.ensureObjectsCap(InitialStorageCapacity)
	r.ensureDrawablesCap(InitialStorageCapacity)
	r.ensureObjectsBindGroup()
}

// collectRenderList populates list by iterating the scene's compact payload tables
// directly, avoiding a full tree traversal. No frustum culling is applied here;
// call cull separately for each frustum.
func (r *Renderer) collectRenderList(list *renderList, scene *Scene) {
	// One pass over the flattened drawable table. Each drawable is gated by its
	// owner node's flags. d.bounds is local; transform it into a world-space
	// sphere for CPU culling in the copy, leaving the stored structural bounds
	// untouched (they're uploaded to the GPU as-is).
	for i := range scene.drawables {
		d := &scene.drawables[i]
		flags := scene.GetFlags(d.ownerNode)
		if !flags.IsAlive() || !flags.IsVisible() {
			continue
		}

		world := scene.world[d.transformID]
		wc := world.Mul4x1(glm.Vec4f{d.bounds.Center[0], d.bounds.Center[1], d.bounds.Center[2], 1})
		entry := *d
		entry.bounds = Sphere{Center: glm.Vec3f{wc[0], wc[1], wc[2]}, Radius: d.bounds.Radius}

		if flags.CastShadow() {
			list.shadowCasters = append(list.shadowCasters, entry)
		}
		list.visible = append(list.visible, entry)
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
func cull(frustum Frustum, src []drawable, dst []drawable) []drawable {
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

func (r *Renderer) ShowFPS(bool) {
	r.showStats = true
}

func (r *Renderer) getSceneState(scene *Scene) *sceneRenderState {
	state, ok := r.sceneStates[scene.id]
	if ok {
		if scene.version != state.gpuVersion {
			r.updateRenderState(state, scene)
		}
		return state
	}

	state = r.createRenderState(scene)
	r.sceneStates[scene.id] = state
	return state
}

func (r *Renderer) createRenderState(scene *Scene) *sceneRenderState {
	state := &sceneRenderState{gpuVersion: scene.version}

	state.cameraUniform = r.gpu.CreateBuffer(gpu.BufferDescriptor{
		Label: "Camera uniform buffer",
		Size:  uint64(unsafe.Sizeof(CameraUniform{})),
		Usage: wgpu.BufferUsageUniform | wgpu.BufferUsageCopyDst,
	})

	state.lightsUniform = r.gpu.CreateBuffer(gpu.BufferDescriptor{
		Label: "Lights uniform buffer",
		Size:  uint64(unsafe.Sizeof(LightsUniform{})),
		Usage: wgpu.BufferUsageUniform | wgpu.BufferUsageCopyDst,
	})

	r.updateRenderState(state, scene)
	return state
}

func (r *Renderer) updateRenderState(state *sceneRenderState, scene *Scene) {
	if state.bindGroup != nil {
		state.bindGroup.Release()
	}

	envMapRef := scene.backgroundMap.ref

	if !envMapRef.Valid() {
		envMapRef = r.defaultTexRef
	}

	envMap := r.textures.Get(envMapRef.id)
	if envMap.gpuVersion < envMap.version {
		r.uploadTexture(envMapRef.id)
	}

	state.bindGroup = r.gpu.CreateBindGroup(wgpu.BindGroupDescriptor{
		Label:  "Global bind group",
		Layout: r.globalBindGroupLayout,
		Entries: []wgpu.BindGroupEntry{
			{
				Binding: CameraBinding,
				Buffer:  r.gpu.RawBuffer(state.cameraUniform),
				Offset:  0,
				Size:    wgpu.WholeSize,
			},
			{
				Binding: LightsBinding,
				Buffer:  r.gpu.RawBuffer(state.lightsUniform),
				Offset:  0,
				Size:    wgpu.WholeSize,
			},
			{
				Binding:     ShadowMapBinding,
				TextureView: r.shadowArray.ArrayView(),
			},
			{
				Binding: ShadowSamplerBinding,
				Sampler: r.shadowArray.Sampler(),
			},
			{
				Binding:     PointShadowMapBinding,
				TextureView: r.pointShadowArray.ArrayView(),
			},
			{
				Binding:     EnvironmentMapBinding,
				TextureView: envMap.gpuView,
			},
			{
				Binding: EnvironmentMapSamplerBinding,
				Sampler: envMap.gpuSampler,
			},
		},
	})

	state.gpuVersion = scene.version
}

type sceneRenderState struct {
	gpuVersion    int
	bindGroup     *wgpu.BindGroup
	cameraUniform gpu.Buffer
	lightsUniform gpu.Buffer
}
