package pix

import (
	"fmt"
	"math"
	"slices"
	"time"
	"unsafe"

	"github.com/bluescreen10/pix/glm"
	"github.com/bluescreen10/pix/gpu"
	"github.com/bluescreen10/pix/shaders"
)

// Renderer is the single entry point. It obtains a gpu backend from the registry
// (so it never imports a concrete backend), owns the shared resources (geometry /
// materials / textures as ref-counted handles) and the GPU-driven pipelines, and
// renders a Scene from a Camera. It renders either to a window swapchain (see the
// platform-specific SetSurface) or, for headless use, to a render target texture
// (SetRenderTarget). There is a single Renderer type for both.
type Renderer struct {
	backend       gpu.Backend
	width, height uint32
	color         gpu.Format
	clear         glm.RGBA32F
	depth         gpu.Texture

	// Presentation target: a swapchain (windowed) or a render-target texture (headless).
	swapchain  gpu.Swapchain
	target     gpu.Texture
	hasTarget  bool
	ownsTarget bool // renderer created the target (via NewOffscreenRenderer)
	readback   gpu.Buffer
	pixels     []byte

	// Renderer-owned shared resources + GPU-driven pipelines. The material stores are
	// owned by the material system, not the renderer.
	geometries *geometrySystem
	materials  *materialSystem
	textures   *textureSystem
	// uploader is shared by every subsystem that stages into device memory; it
	// lives as long as the renderer so its staging arena is reused across frames.
	uploader *uploader

	cullPipeline gpu.Pipeline
	skinPipeline gpu.Pipeline // compute pre-skinning (scene_skin.comp) — see dispatchSkinning
	// skinScratch collects the frame's compute-skinning dispatches (see skinCommands);
	// reused every frame rather than reallocated.
	skinScratch []skinCmd
	// Draw pipelines, one per distinct material pipeline key (shaders + cull + blend).
	// drawPipelineKeys is parallel so pipelineFor can dedup and buildPipelines can rebuild
	// them all when the target format changes. A key's pass says whether it belongs to
	// the G-buffer fill or the forward pass (see encode/issueDraws).
	drawPipelines    []gpu.Pipeline
	drawPipelineKeys []materialPipeline
	pipelinesReady   bool

	// Deferred: global toggle (off by default — every material renders Forward() until
	// enabled) + the G-buffer targets (recreated with the depth buffer on resize) + the
	// plain (non-comparison) sampler used to read them and depth in the lighting pass.
	// lightingPipelines is one fullscreen pipeline per unique Lighting() shader (a
	// shading model); lightingKeys is parallel, for dedup + rebuilds. lightingScratch
	// is reused each frame to collect the models a frame actually references.
	deferredEnabled   bool
	diffuseTexture    gpu.Texture
	normalTexture     gpu.Texture
	materialTexture   gpu.Texture
	emissiveTexture   gpu.Texture
	gbufferSampler    gpu.Sampler
	lightingPipelines []gpu.Pipeline
	lightingKeys      []lightingPipelineKey
	lightingScratch   []uint32

	// Shadows: global toggle + the shared PCF comparison sampler (created lazily) +
	// the position-only depth-pass pipeline (rebuilt with the others on format change).
	// shadowDistance caps how far down the view frustum directional shadows are fit
	// (0 = auto: reach the far side of the scene sphere).
	shadowsEnabled bool
	shadowSampler  gpu.Sampler
	shadowPipeline gpu.Pipeline
	shadowDistance float32

	// Stats / debug HUD.
	stats     *RendererStats
	fontColor glm.RGBA32F
	showFPS   bool
	overlay   *overlay
	gpuPool   gpu.QueryPool
	gpuValid  bool
}

// EnableShadows globally enables or disables shadow rendering. When off, no shadow
// views are culled and no shadow passes run.
func (r *Renderer) EnableShadows(on bool) {
	r.shadowsEnabled = on
}

// EnableDeferredRendering globally toggles the deferred (G-buffer) path. Off by
// default: every material renders through Forward() regardless of whether it also
// supplies Deferred+Lighting. When on, an eligible Opaque material (one
// providing both) renders through the G-buffer instead; everything else still uses
// Forward(). Takes effect on the next Render call (pipeline routing is resolved fresh
// each frame, so no explicit invalidation is needed).
func (r *Renderer) EnableDeferredRendering(on bool) {
	r.deferredEnabled = on
}

// SetShadowDistance caps how far along the camera's view frustum directional shadows
// are fit: a smaller distance packs the shadow map's resolution into the near view for
// sharper shadows, at the cost of no shadows beyond it. Pass 0 for the automatic
// default (fit reaches the far side of the scene's bounding sphere).
func (r *Renderer) SetShadowDistance(d float32) {
	r.shadowDistance = d
}

// NewRenderer creates a renderer from cfg: it selects and initializes a registered
// backend, then configures the presentation target — a window swapchain (cfg.Window)
// or an internal offscreen target (cfg.Width/Height when Window is nil). If neither
// is set, configure one later (SetRenderTarget) before rendering. A nil cfg is the
// zero config.
func NewRenderer(cfg *RendererConfig) (*Renderer, error) {
	if cfg == nil {
		cfg = &RendererConfig{}
	}
	var name *string
	if cfg.Backend != "" {
		name = &cfg.Backend
	}
	backend := gpu.Instance(name)
	if err := backend.Init(); err != nil {
		return nil, fmt.Errorf("render: init backend: %w", err)
	}
	r := &Renderer{
		backend:   backend,
		clear:     glm.RGBA32F{0, 0, 0, 1},      //TODO: extract as constant
		fontColor: glm.RGBA32F{1, 0.9, 0.35, 1}, //TODO: extract as constant
		// One uploader for the process, not one per frame: its staging arena only
		// pays off by keeping its memory across frames (see uploader).
		uploader:   newUploader(backend),
		geometries: newGeometrySystem(backend),
		materials:  newMaterialSystem(backend),
		textures:   newTextureSystem(backend),
		stats:      newRendererStats(60),
	}

	switch {
	case cfg.Window != nil:
		r.attachWindow(cfg.Window, cfg.Width, cfg.Height) // platform-specific
	case cfg.Width > 0 && cfg.Height > 0:
		r.attatchTexture(cfg.Width, cfg.Height)
	}
	return r, nil
}

// attachTexture configures an internally-owned RGBA8 render target of w×h.
func (r *Renderer) attatchTexture(w, h uint32) {
	tex := r.backend.CreateTexture(gpu.TextureDescriptor{Kind: gpu.Texture2D, Width: w, Height: h,
		Format: gpu.FormatRGBA8Unorm, Usage: gpu.TextureRenderTarget | gpu.TextureTransfer})
	r.ownsTarget = true
	r.SetRenderTarget(tex, w, h, gpu.FormatRGBA8Unorm)
}

// NewOffscreenRenderer is a convenience for a headless renderer with an internally-
// owned RGBA8 target of the given size (read it with Pixels/Capture). Equivalent to
// NewRenderer(&RendererConfig{Width, Height}); kept as an ergonomic shorthand.
func NewOffscreenRenderer(w, h uint32) (*Renderer, error) {
	return NewRenderer(&RendererConfig{Width: w, Height: h})
}

// SetRenderTarget renders into tex (headless). The renderer (re)creates its depth
// buffer and pipelines to match. tex must have render-target usage (plus transfer
// usage if you intend to Capture it).
func (r *Renderer) SetRenderTarget(tex gpu.Texture, w, h uint32, format gpu.Format) {
	r.target = tex
	r.hasTarget = true
	// The swapchain handle is left intact: rendering to a target can be temporary, and
	// hasTarget (not a nil swapchain) is what selects the destination — so a later
	// hasTarget=false renders to the window again without recreating the swapchain.
	r.configure(w, h, format)
}

// configure sets the size/format, (re)creates the depth buffer and the pipelines, and
// drops any G-buffer targets so they're re-made at the new size on the next deferred
// frame (see ensureGBuffer — they are not created here, since most renderers never
// enable deferred rendering and they cost 16 bytes/pixel; see gbuffer.glsl).
func (r *Renderer) configure(w, h uint32, format gpu.Format) {
	r.width, r.height, r.color = w, h, format
	if r.depth.Valid() {
		r.backend.DestroyTexture(r.depth)
	}
	// Sampled too: the deferred lighting pass reads depth back to reconstruct position.
	r.depth = r.backend.CreateTexture(gpu.TextureDescriptor{Kind: gpu.Texture2D, Width: w, Height: h,
		Format: gpu.FormatDepth32F, Usage: gpu.TextureDepth | gpu.TextureSampled, Label: "depth"})
	r.destroyGBuffer()
	r.buildPipelines()
}

// ensureGBuffer creates the G-buffer targets at the current size if they aren't already
// (they're dropped on resize by configure). Called on the first deferred frame, so a
// renderer that never enables deferred rendering never allocates them.
//
// NOTE: the backend hands out bindless heap slots monotonically and DestroyTexture does
// not reclaim them, so each resize while deferred is enabled permanently consumes four
// sampled-image slots. Fine for now; reclaiming slots is a backend-wide change.
func (r *Renderer) ensureGBuffer() {
	if r.diffuseTexture.Valid() {
		return
	}
	rt := func(format gpu.Format, label string) gpu.Texture {
		return r.backend.CreateTexture(gpu.TextureDescriptor{Kind: gpu.Texture2D, Width: r.width, Height: r.height,
			Format: format, Usage: gpu.TextureRenderTarget | gpu.TextureSampled, Label: label})
	}
	// See gbuffer.glsl for the channel layout; `material` is model-defined.
	r.diffuseTexture = rt(gpu.FormatRGBA8Unorm, "gbuffer-diffuse")
	r.normalTexture = rt(gpu.FormatRG16F, "gbuffer-normal")
	r.materialTexture = rt(gpu.FormatRGBA8Unorm, "gbuffer-material")
	r.emissiveTexture = rt(gpu.FormatRGBA8Unorm, "gbuffer-emissive")
}

// destroyGBuffer releases the G-buffer targets and marks them absent.
func (r *Renderer) destroyGBuffer() {
	for _, t := range []*gpu.Texture{&r.diffuseTexture, &r.normalTexture, &r.materialTexture, &r.emissiveTexture} {
		if t.Valid() {
			r.backend.DestroyTexture(*t)
			*t = gpu.Texture{}
		}
	}
}

// buildPipelines (re)creates the cull pipeline and every draw pipeline (forward,
// G-buffer, and deferred lighting) from the registered material shaders (called on
// target/format change).
func (r *Renderer) buildPipelines() {
	if r.pipelinesReady {
		r.backend.DestroyPipeline(r.cullPipeline)
		r.backend.DestroyPipeline(r.skinPipeline)
		r.backend.DestroyPipeline(r.shadowPipeline)
		for _, p := range r.drawPipelines {
			r.backend.DestroyPipeline(p)
		}
		for _, p := range r.lightingPipelines {
			r.backend.DestroyPipeline(p)
		}
	}
	r.cullPipeline = r.backend.CreateComputePipeline(gpu.ComputePipelineDescriptor{Shader: shaders.SceneCull, Entry: "main", Label: "scene-cull"})
	r.skinPipeline = r.backend.CreateComputePipeline(gpu.ComputePipelineDescriptor{Shader: shaders.SceneSkin, Entry: "main", Label: "scene-skin"})
	// Shadow depth pass: position-only vertex-pull, no color attachment, writes depth.
	// Cull is disabled so thin geometry still occludes from the light's view.
	r.shadowPipeline = r.backend.CreateGraphicsPipeline(gpu.PipelineDescriptor{
		VertexShader: shaders.SceneShadowVert, FragmentShader: shaders.SceneShadowFrag,
		Topology: gpu.TopologyTriangles, DepthFormat: gpu.FormatDepth32F,
		DepthTest: true, DepthWrite: true, DepthCompare: gpu.CompareLess,
		CullMode: gpu.CullNone, Label: "scene-shadow",
	})
	for i, k := range r.drawPipelineKeys {
		r.drawPipelines[i] = r.buildDrawPipe(k)
	}
	for i, k := range r.lightingKeys {
		r.lightingPipelines[i] = r.buildLightingPipe(k.fragment)
	}
	r.pipelinesReady = true
	if r.overlay != nil {
		r.overlay.destroy()
		r.overlay = newOverlay(r.backend, r.color, gpu.FormatDepth32F)
	}
}

// pass distinguishes a material pipeline's draw pass: forward (shade + output color in
// one pass) or gbuffer (fill the G-buffer; a separate deferred lighting pass shades it).
type pass uint8

const (
	passForward pass = iota
	passGBuffer
)

// materialPipeline is a material's full pipeline identity: shaders + raster state +
// which pass it belongs to. For a gbuffer pipeline, lightingIdx is the index into
// r.lightingPipelines that shades it (materials sharing a Lighting() shader share a
// lighting pipeline — a shading model).
type materialPipeline struct {
	pass             pass
	shaderHash       uint32 // cached (vertex,fragment) identity — the dedup key
	vertex, fragment []byte // kept only to build the pipeline
	cull             CullMode
	blend            BlendMode
	lightingIdx      uint32 // valid iff pass == passGBuffer
}

// buildDrawPipe creates a graphics pipeline for a material pipeline key against the
// current color/depth formats. The vertex-pull stage is shared unless the material
// supplies its own vertex program. A gbuffer-pass key targets the three G-buffer
// formats plus the main color target (for emissive), always opaque; a forward-pass key
// targets the main color target alone, with its material's blend mode.
func (r *Renderer) buildDrawPipe(k materialPipeline) gpu.Pipeline {
	vert := k.vertex
	if vert == nil {
		vert = shaders.SceneDraw
	}
	if k.pass == passGBuffer {
		return r.backend.CreateGraphicsPipeline(gpu.PipelineDescriptor{
			VertexShader: vert, FragmentShader: k.fragment,
			Topology:     gpu.TopologyTriangles,
			ColorFormats: []gpu.Format{gpu.FormatRGBA8Unorm, gpu.FormatRG16F, gpu.FormatRGBA8Unorm, gpu.FormatRGBA8Unorm},
			DepthFormat:  gpu.FormatDepth32F, DepthTest: true, DepthWrite: true, DepthCompare: gpu.CompareLess,
			CullMode: gpu.CullMode(k.cull), FrontFaceCW: true,
		})
	}
	var blend []gpu.BlendState
	switch k.blend {
	case BlendAlpha:
		blend = []gpu.BlendState{{Enable: true, ColorOp: gpu.BlendFactorOp{Src: gpu.BlendSrcAlpha, Dst: gpu.BlendOneMinusSrcAlpha, Op: gpu.BlendAdd}}}
	case BlendAdditive:
		blend = []gpu.BlendState{{Enable: true, ColorOp: gpu.BlendFactorOp{Src: gpu.BlendSrcAlpha, Dst: gpu.BlendOne, Op: gpu.BlendAdd}}}
	}
	// Transparent (blended) materials test depth against opaque geometry but do NOT
	// write depth, so they don't occlude each other and blend correctly.
	depthWrite := k.blend == BlendOpaque
	return r.backend.CreateGraphicsPipeline(gpu.PipelineDescriptor{
		VertexShader: vert, FragmentShader: k.fragment,
		Topology: gpu.TopologyTriangles, ColorFormats: []gpu.Format{r.color},
		DepthFormat: gpu.FormatDepth32F, DepthTest: true, DepthWrite: depthWrite, DepthCompare: gpu.CompareLess,
		// The renderer flips clip-space Y (Vulkan NDC is Y-down), which reverses
		// triangle winding, so front faces are clockwise on screen.
		CullMode: gpu.CullMode(k.cull), FrontFaceCW: true, Blend: blend,
	})
}

// buildLightingPipe creates the fullscreen deferred-lighting pipeline for one shading
// model: no vertex input, and it *replaces* the main color target rather than blending
// (the shader already sums ambient + lighting + emissive in linear space and encodes
// once, so there is nothing to accumulate; the pass clears to the scene clear color, so
// rejected background pixels keep it).
//
// The G-buffer's depth is bound read-only and tested with CompareGreater against the
// fullscreen triangle's far-plane depth (see fullscreen.vert), so the hardware rejects
// background pixels — those still holding the far clear value — before the fragment
// shader runs at all. Without it every pixel in the viewport pays for an invocation
// and a depth sample just to discard.
func (r *Renderer) buildLightingPipe(fragment []byte) gpu.Pipeline {
	return r.backend.CreateGraphicsPipeline(gpu.PipelineDescriptor{
		VertexShader: shaders.FullscreenVert, FragmentShader: fragment,
		Topology: gpu.TopologyTriangles, ColorFormats: []gpu.Format{r.color},
		DepthFormat: gpu.FormatDepth32F, DepthTest: true, DepthWrite: false, DepthCompare: gpu.CompareGreater,
		CullMode: gpu.CullNone,
	})
}

// lightingPipelineKey identifies one deferred shading model: the hash of its Lighting()
// shader (the dedup key) plus the SPIR-V itself, kept only to rebuild the pipeline on a
// format change — the same shape as materialPipeline.
type lightingPipelineKey struct {
	hash     uint32
	fragment []byte
}

// lightingPipelineFor resolves a material's Lighting() shader to a lighting-pipeline
// index, building and caching it the first time a given hash is seen (one fullscreen
// pipeline per unique shading model, shared by every material using that Lighting()).
func (r *Renderer) lightingPipelineFor(lightingShader []byte, hash uint32) uint32 {
	for i := range r.lightingKeys {
		if r.lightingKeys[i].hash == hash {
			return uint32(i)
		}
	}
	id := uint32(len(r.lightingKeys))
	r.lightingKeys = append(r.lightingKeys, lightingPipelineKey{hash: hash, fragment: lightingShader})
	r.lightingPipelines = append(r.lightingPipelines, r.buildLightingPipe(lightingShader))
	return id
}

// pipelineFor resolves a material pipeline key to a draw-pipeline index, building and
// caching a pipeline the first time a given key is seen (shaders deduped by SPIR-V
// slice identity + cull/blend). Custom materials register here too.
func (r *Renderer) pipelineFor(k materialPipeline) uint32 {
	for i := range r.drawPipelineKeys {
		if sameKey(r.drawPipelineKeys[i], k) {
			return uint32(i)
		}
	}
	id := uint32(len(r.drawPipelineKeys))
	r.drawPipelineKeys = append(r.drawPipelineKeys, k)
	r.drawPipelines = append(r.drawPipelines, r.buildDrawPipe(k))
	return id
}

// pipelineForMaterial resolves the draw pipeline for a material from its interface.
// When deferred rendering is enabled (EnableDeferredRendering), an opaque material
// supplying both Deferred and Lighting renders through the G-buffer; everything else
// (blended, deferred disabled, or opaque missing either shader) renders forward.
// Blended materials are forced forward: the G-buffer holds one surface per pixel, so
// it cannot represent a fragment that composites over what is behind it.
func (r *Renderer) pipelineForMaterial(m Material) uint32 {
	if r.deferredEnabled && m.Blend() == BlendOpaque {
		if def, lit := m.Deferred(), m.Lighting(); def != nil && lit != nil {
			return r.pipelineFor(materialPipeline{
				pass: passGBuffer, shaderHash: m.deferredHash(), vertex: m.Vertex(), fragment: def,
				cull: m.Cull(), lightingIdx: r.lightingPipelineFor(lit, m.lightingHash()),
			})
		}
	}
	return r.pipelineFor(materialPipeline{
		pass: passForward, shaderHash: m.shaderHash(), vertex: m.Vertex(), fragment: m.Forward(), cull: m.Cull(), blend: m.Blend(),
	})
}

// sameKey compares two pipeline keys. lightingIdx is part of the identity: a gbuffer
// key hashes only (Vertex, Deferred), so two materials sharing a Deferred shader but
// using different Lighting shaders would otherwise collapse onto one pipeline — and
// recordLighting reads lightingIdx off the stored key, shading the second material's
// pixels with the first one's lighting model.
func sameKey(a, b materialPipeline) bool {
	return a.pass == b.pass && a.shaderHash == b.shaderHash &&
		a.cull == b.cull && a.blend == b.blend && a.lightingIdx == b.lightingIdx
}

// NewGeometry uploads mesh data and returns a ref-counted geometry handle.
func (r *Renderer) NewGeometry(cfg GeometryConfig) Geometry {
	return r.geometries.newHandle(cfg)
}

// The named material constructors (NewBasicMaterial, NewBlinnPhongMaterial,
// NewPBRMaterial) live in their respective *_material.go files.

// NewTexture uploads an RGBA8 image into the bindless heap and returns a handle.
// format selects the color space (TextureSRGB for color maps, TextureLinear for
// data maps like normals/roughness).
func (r *Renderer) NewTexture(pixels []byte, w, h int, format TextureFormat) Texture {
	return r.textures.upload(pixels, w, h, format)
}

// DefaultSampler returns the heap index of the default linear/repeat sampler.
func (r *Renderer) DefaultSampler() uint32 {
	return r.textures.DefaultSampler()
}

// NewScene creates a scene bound to this renderer's backend.
func (r *Renderer) NewScene() *Scene {
	return NewScene(r.backend)
}

// Backend exposes the underlying gpu backend (advanced/one-off use).
func (r *Renderer) Backend() gpu.Backend {
	return r.backend
}

// Size returns the render target size in pixels.
func (r *Renderer) Size() (uint32, uint32) {
	return r.width, r.height
}

// Aspect returns width/height.
func (r *Renderer) Aspect() float32 {
	return float32(r.width) / float32(r.height)
}

// SetClearColor sets the color the framebuffer is cleared to each frame.
func (r *Renderer) SetClearColor(rgba glm.RGBA32F) {
	r.clear = rgba
}

// ShowFPS toggles the debug HUD (FPS + CPU + GPU frame times, in the bitmap font).
func (r *Renderer) ShowFPS(on bool) {
	r.showFPS = on
	if on {
		if r.overlay == nil && r.pipelinesReady {
			r.overlay = newOverlay(r.backend, r.color, gpu.FormatDepth32F)
		}
		if !r.gpuPool.Valid() {
			r.gpuPool = r.backend.CreateTimestampPool(2)
		}
	}
}

// Render draws the scene from cam into the configured target (swapchain or texture).
// The camera is not retained.
func (r *Renderer) Render(scene *Scene, cam Camera) {
	//TODO: r.swapchain.H == 0 is a bit of a smell something like r.swapchain.Valid()
	// would be better
	if !r.hasTarget && r.swapchain.H == 0 {
		panic("renderer has no target")
	}
	r.stats.StartFrame()
	prepStart := time.Now()
	// One command buffer for the whole frame: the staged uploads record into its
	// head and are ordered against the rest by a barrier (see uploader.End), rather
	// than taking a submit + queue drain of their own before the frame even starts.
	// Recording can begin before AcquireNext — the acquire semaphore is waited on at
	// submit time, not at record time.
	cmd := r.backend.Begin()
	r.syncScene(scene, cam, cmd)
	vp := cam.ViewProjection()
	planes := FrustumPlanes(vp)
	drawVP := flipClipY(vp)
	eye := cam.Position()
	if r.showFPS {
		r.buildOverlay()
	}
	cpu := time.Since(prepStart)

	target := r.target

	if !r.hasTarget {
		// Windowed: AcquireNext blocks on vsync (not CPU cost) — outside the timing.
		target, _ = r.backend.AcquireNext(r.swapchain)
	}
	encStart := time.Now()
	r.encode(cmd, target, scene, planes, drawVP, eye)
	cpu += time.Since(encStart)
	r.stats.AddCPUTime(cpu)

	if r.hasTarget {
		// Offscreen: submit the frame ourselves (windowed rendering submits via Present).
		// Capture's readback submits after this on the same queue, so it sees the result.
		r.backend.Wait(r.backend.Submit(cmd))
	} else {
		r.backend.Present(r.swapchain, cmd)
	}

	r.readGPU()
	r.stats.EndFrame()
	return
}

// Capture copies the current render target into a CPU buffer and returns it as RGBA8
// (headless targets only; the target needs transfer usage). Call after Render.
func (r *Renderer) Capture() []byte {
	if !r.hasTarget {
		return nil
	}
	n := int(r.width * r.height * 4)
	if !r.readback.Valid() {
		r.readback = r.backend.Alloc(uint64(n), gpu.MemoryHost, "readback")
		r.pixels = make([]byte, n)
	}
	cmd := r.backend.Begin()
	cmd.CopyTextureToBuffer(r.readback, r.target, 0, 0)
	f := r.backend.Submit(cmd)
	r.backend.Wait(f)
	copy(r.pixels, unsafe.Slice((*byte)(r.readback.Ptr), n))
	return r.pixels
}

// Pixels is an alias for Capture (headless).
func (r *Renderer) Pixels() []byte { return r.Capture() }

// encode records the frame: the shadow views (cull + depth pass per casting light),
// then the main view (cull + lit draw), plus GPU timestamps + the HUD when enabled.
// All compute culls run first (outside any render pass), share one barrier, then the
// shadow depth passes and the main color pass consume their results.
func (r *Renderer) encode(cmd gpu.CommandBuffer, target gpu.Texture, scene *Scene, planes [6][4]float32, drawVP glm.Mat4f, eye glm.Vec3f) {
	if r.showFPS {
		cmd.ResetTimestamps(r.gpuPool, 2)
		cmd.WriteTimestamp(r.gpuPool, 0, gpu.StageNone)
	}
	dl := scene.drawList
	// Shadow views only make sense when there's geometry to cast; with an empty draw
	// list the per-view buffers would be zero-sized (visCap == 0) and there's nothing
	// to render into a depth map anyway, so skip all shadow work.
	var views []shadowView
	if dl.batchCount() > 0 {
		views = r.collectShadowViews(scene)
		if len(views) > 0 {
			dl.ensureShadowViews(len(views))
		}
	}

	// 1. Compute pre-skinning, then cull every view (shadow views filter to
	// casters). Skinning writes positions that the shadow/G-buffer/forward vertex
	// stages read (not cull — it only reads CPU-supplied bounds), so one barrier
	// after both covers everything.
	if dl.batchCount() > 0 {
		if cmds := r.skinCommands(scene); len(cmds) > 0 {
			dl.ensureSkinRoots(len(cmds))
			r.dispatchSkinning(cmd, dl, cmds, scene.jointBuf.Addr)
		}
		for i := range views {
			v := &dl.shadowViews[i]
			sp := FrustumPlanes(views[i].cam.ViewProjection())
			r.cullInto(cmd, dl, v.indirectBuf, v.visibleBuf, v.cullRootBuf, sp, 1)
		}
		r.cullInto(cmd, dl, dl.indirectBuf, dl.visibleBuf, dl.cullRootBuf, planes, 0)
		cmd.Barrier(gpu.StageCompute, gpu.StageIndirect|gpu.StageVertex, 0)
	}

	// 2. Shadow depth passes: fill each view's map, then hand it to the samplers.
	for i := range views {
		r.recordShadowDepth(cmd, dl, &dl.shadowViews[i], views[i])
		//FIXME: use VK_KHR_unified_image_layouts
		// this should remove the PreparedSampled from the RHI API
		cmd.PrepareSampled(r.textures.gpu(views[i].m), gpu.StageFragment)
	}

	// 3. Every run's drawRoot is filled once regardless of pass (same camera either
	// way); issueDraws below then filters by pass to route it into the right render
	// pass with the right pipeline set.
	r.fillDrawRoots(dl, drawVP, eye, scene.lights.Addr())
	gbufferActive := r.hasGBufferRuns(dl)

	// 4. G-buffer fill + deferred lighting, only when something renders that way.
	if gbufferActive {
		r.ensureGBuffer()
		cmd.BeginRenderPass(gpu.RenderTargets{
			Color: []gpu.ColorAttachment{
				{Texture: r.diffuseTexture, Load: gpu.LoadClear},
				{Texture: r.normalTexture, Load: gpu.LoadClear},
				{Texture: r.materialTexture, Load: gpu.LoadClear},
				{Texture: r.emissiveTexture, Load: gpu.LoadClear},
			},
			Depth: &gpu.DepthAttachment{Texture: r.depth, Load: gpu.LoadClear, Clear: 1.0},
		})
		cmd.Viewport(0, 0, float32(r.width), float32(r.height), 0, 1)
		cmd.Scissor(0, 0, int32(r.width), int32(r.height))
		r.issueDraws(cmd, dl, passGBuffer)
		cmd.EndRenderPass()

		cmd.PrepareSampled(r.diffuseTexture, gpu.StageFragment)
		cmd.PrepareSampled(r.normalTexture, gpu.StageFragment)
		cmd.PrepareSampled(r.materialTexture, gpu.StageFragment)
		cmd.PrepareSampled(r.emissiveTexture, gpu.StageFragment)
		cmd.PrepareSampled(r.depth, gpu.StageFragment)
		r.recordLighting(cmd, dl, target, drawVP, eye, scene.lights.Addr())
	}

	// 5. Forward pass: transparents, and any Opaque material that did not
	// qualify for deferred. If the G-buffer ran, its color+depth are loaded (not
	// cleared) so forward geometry composites over the already-lit scene and depth-
	// tests correctly against it; otherwise this is the only pass, so it clears.
	colorLoad, depthLoad := gpu.LoadClear, gpu.LoadClear
	if gbufferActive {
		colorLoad, depthLoad = gpu.LoadKeep, gpu.LoadKeep
	}
	cmd.BeginRenderPass(gpu.RenderTargets{
		Color: []gpu.ColorAttachment{{Texture: target, Load: colorLoad, Clear: r.clear}},
		Depth: &gpu.DepthAttachment{Texture: r.depth, Load: depthLoad, Clear: 1.0},
	})
	cmd.Viewport(0, 0, float32(r.width), float32(r.height), 0, 1)
	cmd.Scissor(0, 0, int32(r.width), int32(r.height))
	r.issueDraws(cmd, dl, passForward)
	if r.showFPS && r.overlay != nil {
		r.overlay.draw(cmd, float32(r.width), float32(r.height))
	}
	cmd.EndRenderPass()
	if r.showFPS {
		cmd.WriteTimestamp(r.gpuPool, 1, gpu.StageColorOutput)
	}
}

// hasGBufferRuns reports whether any of the draw list's current pipeline runs belong
// to the G-buffer pass (i.e. some drawable's material is rendering deferred this frame).
func (r *Renderer) hasGBufferRuns(dl *drawList) bool {
	for i := range dl.runs {
		if r.drawPipelineKeys[dl.runs[i].pipeline].pass == passGBuffer {
			return true
		}
	}
	return false
}

// shadowView is one depth-map render request: a camera, the map it renders into, and
// its resolution. Directional and spot lights contribute one each; a point light
// contributes six (its cube faces). The renderer treats them all uniformly — cull with
// castersOnly, depth pass, hand to the samplers — so light types don't leak into the
// GPU-driven path.
type shadowView struct {
	cam  Camera
	m    Texture
	size uint32
}

// collectShadowViews gathers every shadow-map render request in the scene: one per
// casting directional/spot light and six per casting point light (Stage 3). Empty when
// shadows are disabled or nothing casts.
func (r *Renderer) collectShadowViews(scene *Scene) []shadowView {
	if !r.shadowsEnabled {
		return nil
	}
	var out []shadowView
	for _, l := range scene.dirLights {
		if s := l.shadow; s != nil && s.Map.Valid() {
			out = append(out, shadowView{cam: s.Camera, m: s.Map, size: s.Size()})
		}
	}
	for _, l := range scene.spotLights {
		if s := l.shadow; s != nil && s.Map.Valid() {
			out = append(out, shadowView{cam: s.Camera, m: s.Map, size: s.Size()})
		}
	}
	for _, l := range scene.pointLights {
		s := l.shadow
		if s == nil {
			continue
		}
		for i := range s.faces {
			if f := s.faces[i]; f.m.Valid() {
				out = append(out, shadowView{cam: f.cam, m: f.m, size: s.Size()})
			}
		}
	}
	return out
}

// syncScene uploads the renderer's shared tables + the scene's per-scene GPU state.
// It resolves each mesh's draw pipeline from its material (shaders + raster) every
// frame and rebuilds the draw list when the mesh set OR any pipeline assignment
// changed (so a material raster/blend change re-batches without an explicit dirty).
func (r *Renderer) syncScene(scene *Scene, cam Camera, cmd gpu.CommandBuffer) {
	// Device-only resources (geometry streams + descriptors, material records) are
	// staged through the shared uploader, which records the copies into the head of
	// this frame's command buffer and barriers them against the passes that read
	// them — no separate submit. The scene owns transforms, skinning, its draw list
	// and lights, and writes those straight to MemoryHost buffers (see Scene.Sync),
	// so a frame where only scene-owned state changed (a refit shadow camera, an
	// animated pose) stages nothing at all and End skips even the barrier.
	if r.shadowsEnabled {
		r.prepareShadows(scene, cam)
	}
	up := r.uploader
	up.Begin(cmd)
	r.geometries.Sync(up)
	r.materials.Sync(up)
	scene.Sync()
	up.End(cmd)

	dl := scene.drawList
	dl.pipeBuf = dl.pipeBuf[:0]
	for i := range scene.meshes {
		//TODO: maybe the material should have a []pipelineKey (or pipelineID) stored
		dl.pipeBuf = append(dl.pipeBuf, r.pipelineForMaterial(scene.meshes[i].material))
	}
	// Order must match collectDrawables (meshes, then skinnedMeshes.All()) — both
	// calls run back-to-back here with no scene mutation between them, so the slab
	// iteration order is identical.
	for _, sm := range scene.skinnedMeshes.All() {
		dl.pipeBuf = append(dl.pipeBuf, r.pipelineForMaterial(sm.material))
	}
	if scene.drawableDirty || !slices.Equal(dl.pipeBuf, dl.batchedPipelines) {
		drawables, materials := scene.collectDrawables()
		dl.rebuild(drawables, dl.pipeBuf, materials, r.geometries)
		scene.drawableDirty = false
	}
}

// prepareShadows ensures each shadow-casting directional light has a depth map and an
// orthographic camera fitted to the camera's view frustum. Runs before the scene
// derives its light table, so the table can carry each light's shadow map index +
// view-projection.
func (r *Renderer) prepareShadows(scene *Scene, cam Camera) {
	if r.shadowSampler.H == 0 {
		r.shadowSampler = r.backend.CreateSampler(gpu.SamplerDescriptor{
			MinLinear: true, MagLinear: true,
			AddressU: gpu.AddressClamp, AddressV: gpu.AddressClamp,
			Compare: gpu.CompareLessEqual, // PCF comparison sampler
			Label:   "shadow-cmp",
		})
	}
	for _, l := range scene.pointLights {
		s := l.shadow
		if s == nil {
			continue
		}
		aimPointShadow(r.textures, s, l)
	}
	if len(scene.dirLights) == 0 && len(scene.spotLights) == 0 {
		return
	}
	sceneCenter, sceneRadius := scene.FrameSphere(1.0)
	corners := frustumCornersWorld(cam.ViewProjection())
	for _, l := range scene.dirLights {
		s := l.shadow
		if s == nil {
			continue
		}
		s.ensureMap(r.textures)
		r.fitDirectionalShadow(s, l.Direction, cam.Position(), corners, sceneCenter, sceneRadius)
	}
	for _, l := range scene.spotLights {
		s := l.shadow
		if s == nil {
			continue
		}
		s.ensureMap(r.textures)
		aimSpotShadow(s, l)
	}
}

// aimPointShadow allocates (once) and re-aims a point light's six cube-face shadow
// cameras from the light's current position and range.
func aimPointShadow(tex *textureSystem, s *LightShadow, l *PointLight) {
	s.updateLocalBias(l.Range)
	s.ensureFaceMaps(tex)
	for i := range s.faces {
		f := &s.faces[i]
		c, ok := f.cam.(interface {
			SetPosition(glm.Vec3f)
			SetTarget(glm.Vec3f)
			SetUp(glm.Vec3f)
			SetFar(float32)
		})
		if !ok {
			continue
		}
		c.SetPosition(l.Position)
		c.SetTarget(l.Position.Add(cubeFaceDirs[i]))
		c.SetUp(cubeFaceUps[i])
		c.SetFar(l.Range)
	}
}

// aimSpotShadow re-aims a spot light's perspective shadow camera from the light's
// current position/direction/cone/range (all mutable each frame).
func aimSpotShadow(s *LightShadow, l *SpotLight) {
	c, ok := s.Camera.(interface {
		SetPosition(glm.Vec3f)
		SetTarget(glm.Vec3f)
		SetUp(glm.Vec3f)
		SetFOV(float32)
		SetFar(float32)
	})
	if !ok {
		return
	}
	d := l.Direction.Normalize()
	up := glm.Vec3f{0, 1, 0}
	if abs32(d.Dot(up)) > 0.99 {
		up = glm.Vec3f{0, 0, 1}
	}
	c.SetPosition(l.Position)
	c.SetTarget(l.Position.Add(d))
	c.SetUp(up)
	c.SetFOV(glm.ToDegrees(2 * l.Angle))
	c.SetFar(l.Range)
	s.updateLocalBias(l.Range)
}

// frustumCornersWorld returns the 8 world-space corners of the frustum described by
// viewProj (indices 0..3 near, 4..7 far). NDC is Vulkan's: xy in [-1,1], z in [0,1].
func frustumCornersWorld(viewProj glm.Mat4f) [8]glm.Vec3f {
	inv := viewProj.Inv()
	var c [8]glm.Vec3f
	i := 0
	for _, z := range [2]float32{0, 1} {
		for _, y := range [2]float32{-1, 1} {
			for _, x := range [2]float32{-1, 1} {
				p := inv.Mul4x1(glm.Vec4f{x, y, z, 1})
				c[i] = glm.Vec3f{p[0] / p[3], p[1] / p[3], p[2] / p[3]}
				i++
			}
		}
	}
	return c
}

// fitDirectionalShadow aims the light's orthographic camera to cover the slice of the
// view frustum within the shadow distance, sized to enclose it. When that slice is as
// large as the whole scene (zoomed out) it falls back to the scene sphere, so it's
// never worse than a whole-scene fit; zoomed in, it packs resolution into the near
// view. The eye is pulled back along -dir across the scene so occluders between the
// light and the frustum are still captured, and the center is snapped to the shadow
// texel grid so edges don't crawl as the camera moves.
func (r *Renderer) fitDirectionalShadow(s *LightShadow, dir, eye glm.Vec3f, corners [8]glm.Vec3f, sceneCenter glm.Vec3f, sceneRadius float32) {
	f, ok := s.Camera.(interface {
		SetPosition(glm.Vec3f)
		SetTarget(glm.Vec3f)
		SetUp(glm.Vec3f)
		SetFrustum(l, r, b, t float32)
		SetClip(near, far float32)
	})
	if !ok {
		return
	}
	d := dir.Normalize()

	// Cap the frustum slice at the shadow distance by pulling each far corner back along
	// its near→far edge. The default tracks the camera's distance to the scene center
	// (so dollying in shrinks the box → more texels per pixel) rather than always
	// spanning the whole scene depth. A small floor keeps the box from collapsing when
	// the camera sits right on the scene. Override with Renderer.SetShadowDistance.
	shadowDist := r.shadowDistance
	if shadowDist <= 0 {
		shadowDist = max(eye.Sub(sceneCenter).Length(), sceneRadius*0.15)
	}
	nearCenter := avgCorners(corners, 0)
	farCenter := avgCorners(corners, 4)
	nearDist := nearCenter.Sub(eye).Length()
	farDist := farCenter.Sub(eye).Length()
	t := float32(1)
	if farDist > nearDist {
		t = glm.Clamp((shadowDist-nearDist)/(farDist-nearDist), 0, 1)
	}
	var pts [8]glm.Vec3f
	for j := 0; j < 4; j++ {
		pts[j] = corners[j]
		pts[j+4] = corners[j].Add(corners[j+4].Sub(corners[j]).Scale(t))
	}

	// Bounding sphere of the capped slice; fall back to the scene sphere when the fit
	// isn't tighter (e.g. zoomed out), so we never do worse than whole-scene.
	center, radius := boundingSphere(pts)
	if radius >= sceneRadius {
		center, radius = sceneCenter, sceneRadius
	}

	// Quantize the radius to a fixed grid so the texel size only changes in small,
	// discrete steps as the camera zooms. A continuously-resizing box would keep moving
	// the texel grid under the geometry, which is what makes the edges crawl — snapping
	// the center only helps while the texel size holds still.
	if step := sceneRadius / 64; step > 0 {
		radius = float32(math.Ceil(float64(radius/step))) * step
	}

	// A stable light basis (avoid the degenerate LookAt when dir ∥ world-up).
	up := glm.Vec3f{0, 1, 0}
	if abs32(d.Dot(up)) > 0.99 {
		up = glm.Vec3f{0, 0, 1}
	}
	right := up.Cross(d).Normalize()
	up = d.Cross(right).Normalize()

	// Snap the center to the shadow texel grid in the light's right/up plane.
	texel := 2 * radius / float32(s.Size())
	if texel > 0 {
		cx := snap(center.Dot(right), texel)
		cy := snap(center.Dot(up), texel)
		cz := center.Dot(d)
		center = right.Scale(cx).Add(up.Scale(cy)).Add(d.Scale(cz))
	}

	const near, farScale = float32(0.01), float32(4)
	far := sceneRadius * farScale
	f.SetUp(up)
	f.SetPosition(center.Sub(d.Scale(sceneRadius * 2))) // back up across the scene
	f.SetTarget(center)
	f.SetFrustum(-radius, radius, -radius, radius)
	f.SetClip(near, far) // depth spans the whole scene toward the light

	// The renderer decides where the camera goes; the shadow owns the derived bias.
	s.updateOrthoBias(radius, far-near)
}

// avgCorners averages the 4 frustum corners starting at base (0 = near, 4 = far).
func avgCorners(c [8]glm.Vec3f, base int) glm.Vec3f {
	sum := c[base].Add(c[base+1]).Add(c[base+2]).Add(c[base+3])
	return sum.Scale(0.25)
}

// boundingSphere returns a center + radius enclosing the 8 points (centroid + max
// distance; not minimal, but stable under rotation, which matters for shadow shimmer).
func boundingSphere(p [8]glm.Vec3f) (glm.Vec3f, float32) {
	var center glm.Vec3f
	for _, v := range p {
		center = center.Add(v)
	}
	center = center.Scale(1.0 / 8.0)
	var radius float32
	for _, v := range p {
		d := v.Sub(center)
		if dsq := d.Dot(d); dsq > radius {
			radius = dsq
		}
	}
	return center, sqrt32(radius)
}

// small float32 math helpers for the shadow fit.
func abs32(x float32) float32 {
	return float32(math.Abs(float64(x)))
}
func sqrt32(x float32) float32 {
	return float32(math.Sqrt(float64(x)))
}
func cos32(x float32) float32 {
	return float32(math.Cos(float64(x)))
}

// snap rounds x down to the nearest multiple of step (for texel-grid alignment).
func snap(x, step float32) float32 {
	return float32(math.Floor(float64(x/step))) * step
}

// cullInto dispatches the frustum-cull compute into one view's indirect + visible
// buffers: it resets the indirect args from the shared template (zeroing each batch's
// instanceCount), points a cull root at this view's buffers (the drawable/world/region
// tables are shared), and dispatches one thread per instance. castersOnly=1 restricts
// the view to shadow casters. No barrier — the caller batches all views behind one.
func (r *Renderer) cullInto(cmd gpu.CommandBuffer, dl *drawList, indirect, visible, root gpu.Buffer, planes [6][4]float32, castersOnly uint32) {
	writeAt(indirect, 0, toBytes(dl.template))
	*(*cullRoot)(root.Ptr) = cullRoot{
		drawables: dl.drawableBuf.Addr, models: dl.worldBuf.Addr, indirect: indirect.Addr,
		regions: dl.regionBuf.Addr, visible: visible.Addr, count: dl.numInst,
		castersOnly: castersOnly, planes: planes,
	}
	cmd.SetPipeline(r.cullPipeline)
	cmd.Root(root.Addr)
	cmd.Dispatch((dl.numInst+63)/64, 1, 1)
}

// skinCommands builds this frame's compute-skinning dispatch list, one entry per
// SkinnedMesh, into a scratch slice reused across frames. The scene supplies the
// state (which meshes are skinned, their source/output geometry, their skeleton's
// joint range); deciding what GPU work that implies is the renderer's job, like
// every other command list it builds here.
//
// Every skinned mesh is dispatched regardless of visibility — a v1 simplification;
// a scene with many off-screen skinned meshes pays for all of them.
func (r *Renderer) skinCommands(scene *Scene) []skinCmd {
	r.skinScratch = r.skinScratch[:0]
	for _, sm := range scene.skinnedMeshes.All() {
		sk := scene.skeletons.Get(sm.skeleton)
		r.skinScratch = append(r.skinScratch, skinCmd{
			srcDesc: sm.srcGeometry.id(), dstDesc: sm.outputGeo.id(),
			jointBase: sk.jointBase, vertexCount: sm.vertCount,
		})
	}
	return r.skinScratch
}

// dispatchSkinning fills one skinRoot per skinned mesh (position/attribute/skin/
// descriptor streams and the scene's joint buffer are frame-global; only
// srcDesc/dstDesc/jointBase/vertexCount vary per mesh) and issues one Dispatch per
// mesh, sized to its vertex count. See scene_skin.comp.
func (r *Renderer) dispatchSkinning(cmd gpu.CommandBuffer, dl *drawList, cmds []skinCmd, jointsAddr uint64) {
	roots := unsafe.Slice((*skinRoot)(dl.skinRootBuf.Ptr), len(cmds))
	posAddr, attrAddr := r.geometries.PositionsAddr(), r.geometries.AttributesAddr()
	skinAddr, descAddr := r.geometries.SkinAddr(), r.geometries.DescriptorsAddr()
	cmd.SetPipeline(r.skinPipeline)
	for i, c := range cmds {
		roots[i] = skinRoot{
			pos: posAddr, attr: attrAddr, skin: skinAddr, descs: descAddr, joints: jointsAddr,
			srcDesc: c.srcDesc, dstDesc: c.dstDesc, jointBase: c.jointBase, vertexCount: c.vertexCount,
		}
		cmd.Root(dl.skinRootBuf.Addr + uint64(i)*skinRootSize)
		cmd.Dispatch((c.vertexCount+63)/64, 1, 1)
	}
}

// recordShadowDepth renders the caster geometry into a light's depth map from its
// camera. It uses the view's own compacted-visible buffer (culled with castersOnly)
// and a single position-only MDI over every batch — material/pipeline don't matter
// for depth, so all batches draw with the one shadow pipeline.
func (r *Renderer) recordShadowDepth(cmd gpu.CommandBuffer, dl *drawList, v *drawView, sv shadowView) {
	size := sv.size
	*(*shadowRoot)(v.drawRootBuf.Ptr) = shadowRoot{
		viewProj:  sv.cam.ViewProjection(),
		pos:       r.geometries.PositionsAddr(),
		descs:     r.geometries.DescriptorsAddr(),
		models:    dl.worldBuf.Addr,
		drawables: dl.drawableBuf.Addr,
		visible:   v.visibleBuf.Addr,
	}
	cmd.BeginRenderPass(gpu.RenderTargets{
		Depth: &gpu.DepthAttachment{Texture: r.textures.gpu(sv.m), Load: gpu.LoadClear, Clear: 1.0},
	})
	cmd.Viewport(0, 0, float32(size), float32(size), 0, 1)
	cmd.Scissor(0, 0, int32(size), int32(size))
	cmd.SetPipeline(r.shadowPipeline)
	cmd.Root(v.drawRootBuf.Addr)
	cmd.DrawIndexedIndirect(r.geometries.IndexBuffer(), v.indirectBuf, 0, uint32(dl.batchCount()), indirectSize)
	cmd.EndRenderPass()
}

// fillDrawRoots writes each pipeline run's drawRoot (the same layout serves both the
// forward and the G-buffer pass — a gbuffer fragment shader just doesn't read
// lights/eye/shadowSampler). Filling every run regardless of pass keeps this call
// independent of which passes actually run this frame; issueDraws filters by pass.
func (r *Renderer) fillDrawRoots(dl *drawList, viewProj glm.Mat4f, eye glm.Vec3f, lightsAddr uint64) {
	if len(dl.runs) == 0 {
		return
	}
	roots := unsafe.Slice((*drawRoot)(dl.drawRootBuf.Ptr), len(dl.runs))
	for ri := range dl.runs {
		run := &dl.runs[ri]
		roots[ri] = drawRoot{
			viewProj:      viewProj,
			pos:           r.geometries.PositionsAddr(),
			attr:          r.geometries.AttributesAddr(),
			descs:         r.geometries.DescriptorsAddr(),
			models:        dl.worldBuf.Addr,
			drawables:     dl.drawableBuf.Addr,
			visible:       dl.visibleBuf.Addr,
			materials:     run.mat.materialsAddr(),
			lights:        lightsAddr,
			eye:           glm.Vec4f{eye[0], eye[1], eye[2], 1},
			shadowSampler: r.shadowSampler.Index,
		}
	}
}

// issueDraws records one multi-draw-indirect call per pipeline run belonging to pass:
// all of a material type's per-geometry commands go in a single DrawIndexedIndirect.
// Each command's firstInstance is its region base, so gl_InstanceIndex indexes the
// compacted visible buffer directly — no per-command push constant.
func (r *Renderer) issueDraws(cmd gpu.CommandBuffer, dl *drawList, p pass) {
	if len(dl.runs) == 0 {
		return
	}
	idx := r.geometries.IndexBuffer()
	for ri := range dl.runs {
		run := &dl.runs[ri]
		if r.drawPipelineKeys[run.pipeline].pass != p {
			continue
		}
		cmd.SetPipeline(r.drawPipelines[run.pipeline])
		cmd.Root(dl.drawRootBuf.Addr + uint64(ri)*drawRootSize)
		cmd.DrawIndexedIndirect(idx, dl.indirectBuf, uint64(run.firstBatch)*uint64(indirectSize), run.count, indirectSize)
	}
}

// recordLighting shades the G-buffer: one fullscreen pass per unique shading model
// referenced by this frame's active gbuffer runs, each additively blending its
// contribution onto target (which already holds the G-buffer pass's emissive write).
//
// NOTE: with more than one active model this shades every non-background pixel in
// every pass (no per-model masking yet — fine while PBR is the only built-in deferred
// model; a real fix, e.g. a model-id compare or a stencil mask written during the
// G-buffer pass, is needed before a second one ships).
func (r *Renderer) recordLighting(cmd gpu.CommandBuffer, dl *drawList, target gpu.Texture, viewProj glm.Mat4f, eye glm.Vec3f, lightsAddr uint64) {
	if r.gbufferSampler.H == 0 {
		// Plain (non-comparison) sampler for reading the G-buffer + depth back as
		// textures — distinct from shadowSampler, which is compare-mode-only and
		// would return comparison results instead of raw values if reused here.
		//
		// Point filtering, deliberately: this pass reads texel-for-texel, and the data
		// is not filterable — interpolating an octahedral normal, a packed model id or
		// a depth value between texels is meaningless. Linear filtering on a D32_SFLOAT
		// image is also not guaranteed to be supported.
		r.gbufferSampler = r.backend.CreateSampler(gpu.SamplerDescriptor{
			AddressU: gpu.AddressClamp, AddressV: gpu.AddressClamp,
			Label: "gbuffer-read",
		})
	}
	invViewProj := viewProj.Inv()
	*(*lightingRoot)(dl.lightingRootBuf.Ptr) = lightingRoot{
		invViewProj:     invViewProj,
		eye:             glm.Vec4f{eye[0], eye[1], eye[2], 1},
		lights:          lightsAddr,
		shadowSampler:   r.shadowSampler.Index,
		gbufferSampler:  r.gbufferSampler.Index,
		diffuseTexture:  r.diffuseTexture.Index,
		normalTexture:   r.normalTexture.Index,
		materialTexture: r.materialTexture.Index,
		emissiveTexture: r.emissiveTexture.Index,
		depthTexture:    r.depth.Index,
		screen:          [2]float32{float32(r.width), float32(r.height)},
	}
	// One pass per distinct shading model referenced this frame (scratch is reused, so
	// no per-frame allocation once it has grown to the model count).
	r.lightingScratch = r.lightingScratch[:0]
	for i := range dl.runs {
		k := r.drawPipelineKeys[dl.runs[i].pipeline]
		if k.pass != passGBuffer || slices.Contains(r.lightingScratch, k.lightingIdx) {
			continue
		}
		r.lightingScratch = append(r.lightingScratch, k.lightingIdx)
		cmd.BeginRenderPass(gpu.RenderTargets{
			Color: []gpu.ColorAttachment{{Texture: target, Load: gpu.LoadClear, Clear: r.clear}},
			Depth: &gpu.DepthAttachment{Texture: r.depth, Load: gpu.LoadKeep, ReadOnly: true},
		})
		cmd.Viewport(0, 0, float32(r.width), float32(r.height), 0, 1)
		cmd.Scissor(0, 0, int32(r.width), int32(r.height))
		cmd.SetPipeline(r.lightingPipelines[k.lightingIdx])
		cmd.Root(dl.lightingRootBuf.Addr)
		cmd.Draw(3, 1, 0, 0)
		cmd.EndRenderPass()
	}
}

func (r *Renderer) buildOverlay() {
	if r.overlay == nil {
		return
	}
	r.overlay.reset()
	ms := func(d time.Duration) float64 { return float64(d.Microseconds()) / 1000 }
	r.overlay.text(fmt.Sprintf("FPS %.0f", r.stats.FPS()), 12, 12, 26, r.fontColor)
	r.overlay.text(fmt.Sprintf("CPU %.2f ms", ms(r.stats.AvgCPUTime())), 12, 44, 26, r.fontColor)
	if r.gpuValid {
		r.overlay.text(fmt.Sprintf("GPU %.2f ms", ms(r.stats.AvgGPUTime())), 12, 76, 26, r.fontColor)
	}
}

func (r *Renderer) readGPU() {
	if !r.showFPS {
		return
	}
	ts := r.backend.ReadTimestamps(r.gpuPool, 2)
	if ts == nil || ts[1] <= ts[0] {
		return
	}
	ns := float64(ts[1]-ts[0]) * r.backend.TimestampPeriod()
	r.stats.AddGPUTime(ns / 1e9)
	r.gpuValid = true
}

// flipClipY negates clip-space Y (proj[1][1]*=-1) so images are right-side-up under
// Vulkan's Y-down NDC. Only screen position is affected.
func flipClipY(m glm.Mat4f) glm.Mat4f {
	m[1], m[5], m[9], m[13] = -m[1], -m[5], -m[9], -m[13]
	return m
}

// Destroy releases the renderer's GPU resources and the backend it owns.
func (r *Renderer) Destroy() {
	if r.overlay != nil {
		r.overlay.destroy()
	}
	if r.gpuPool.Valid() {
		r.backend.DestroyTimestampPool(r.gpuPool)
	}
	if r.pipelinesReady {
		r.backend.DestroyPipeline(r.cullPipeline)
		r.backend.DestroyPipeline(r.skinPipeline)
		r.backend.DestroyPipeline(r.shadowPipeline)
		for _, p := range r.drawPipelines {
			r.backend.DestroyPipeline(p)
		}
		for _, p := range r.lightingPipelines {
			r.backend.DestroyPipeline(p)
		}
	}
	if r.depth.Valid() {
		r.backend.DestroyTexture(r.depth)
	}
	r.destroyGBuffer()
	if r.gbufferSampler.H != 0 {
		r.backend.DestroySampler(r.gbufferSampler)
	}
	if r.shadowSampler.H != 0 {
		r.backend.DestroySampler(r.shadowSampler)
	}
	if r.ownsTarget && r.target.Valid() {
		r.backend.DestroyTexture(r.target)
	}
	if r.readback.Valid() {
		r.backend.Free(r.readback)
	}
	r.geometries.Destroy()
	r.materials.Destroy()
	r.textures.Destroy()
	r.uploader.destroy()
	r.backend.Destroy()
}

//TODO: Add a resize method that would adjust the SwapChain size
