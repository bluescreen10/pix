package pix

import (
	"fmt"
	"os"
	"slices"
	"time"
	"unsafe"

	"github.com/bluescreen10/gamekit/gpu"
	"github.com/bluescreen10/gamekit/utils"
	"github.com/bluescreen10/pix/colors"
	"github.com/bluescreen10/pix/console"
	"github.com/bluescreen10/pix/geometries"
	"github.com/bluescreen10/pix/glm"
	"github.com/bluescreen10/pix/materials"
	"github.com/bluescreen10/pix/scenes"
	"github.com/bluescreen10/pix/shaders"
	"github.com/bluescreen10/pix/textures"
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
	scale         float32 // framebuffer pixels per logical point (see RendererConfig.Scale)
	color         gpu.Format
	clear         colors.RGBA32F
	depth         gpu.Texture

	// Presentation target: a swapchain (windowed) or a render-target texture (headless).
	swapchain  gpu.Swapchain
	target     gpu.Texture
	hasTarget  bool
	ownsTarget bool // renderer created the target (via NewOffscreenRenderer)
	readback   gpu.Buffer
	pixels     []byte

	// Renderer-owned shared resources + GPU-driven pipelines. All three stores are
	// exported: callers create resources on them directly (GeometryStore.Create,
	// TextureStore.Create, r.NewPBRMaterial()) rather than through per-renderer
	// wrapper methods that would have to be kept in sync.
	GeometryStore *geometries.Store
	TextureStore  *textures.Store
	MaterialStore *materials.Store
	// uploader is shared by every subsystem that stages into device memory; it
	// lives as long as the renderer so its staging arena is reused across frames.
	uploader *uploader

	cullPipeline           gpu.Pipeline
	skinPipeline           gpu.Pipeline // compute pre-skinning (scene_skin.comp) — see dispatchSkinning
	particleUpdatePipeline gpu.Pipeline // compute particle simulation (particle_update.comp) — see dispatchParticleUpdate
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
	// frame is the scratch packet every extraction fills. It is reused rather than
	// freshly declared per frame so its tables keep their capacity: extracting into it
	// allocates nothing in the steady state.
	frame scenes.FramePacket
	// sources is the renderer's GPU state, one entry per producer it has drawn. It is
	// a cache keyed by identity, not a second scene graph: it holds no hierarchy and
	// cannot read application objects to fill anything in. A producer that goes away
	// leaves its entry behind until ReleaseSource is called for its id — the renderer
	// is never told about a Scene being destroyed.
	sources map[scenes.SourceID]*renderState
	// pools indexes poolPipelines by materials.Pool.Index, so resolving a material's
	// pipeline is an array index rather than a scan of drawPipelineKeys. Grown on
	// demand; entries stay valid across a format change, which rebuilds the pipelines
	// in place and leaves their indices alone.
	pools []poolPipelines

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

	// debugView displays one G-buffer target instead of the shaded frame; its pipeline
	// is built on first use since most frames never need it (see debug_view.go).
	debugView       DebugView
	debugPipeline   gpu.Pipeline
	debugIDPipeline gpu.Pipeline // DebugObjectID/DebugTriangleID's own pipeline (see recordDebugIDView)

	// Shadows: global toggle + the shared PCF comparison sampler (created lazily) +
	// the position-only depth-pass pipeline (rebuilt with the others on format change).
	// shadowDistance caps how far down the view frustum directional shadows are fit
	// (0 = auto: reach the far side of the scene sphere).
	// shadowAlgorithm picks how directional shadow cameras are fitted (see
	// ShadowAlgorithm and shadow_fit.go). Spot and point lights ignore it.
	shadowsEnabled bool
	shadowSampler  gpu.Sampler
	shadowPipeline gpu.Pipeline
	shadowDistance float32
	shadowNear     float32
	shadowFilter   ShadowFilter
	// shadows is how directional lights are fitted plus that fit's own settings; nil
	// means ShadowUniform (see Renderer.Shadows).
	shadows ShadowSettings

	// pendingShot is a queued frame capture, recorded into the frame being built (see
	// screenshot.go).
	pendingShot *screenshot

	// Console: nil until EnableConsole. It shares the debug overlay with the FPS HUD,
	// so either one being active is what keeps the overlay alive.
	console *console.Console

	// Stats / debug HUD.
	stats     *RendererStats
	fontColor colors.RGBA32F
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

// Scale is framebuffer pixels per logical point (see RendererConfig.Scale). Sizes the
// renderer draws for a human to read — the HUD, the console — are given in logical
// points and multiplied by this, so they stay the same physical size on any display.
func (r *Renderer) Scale() float32 {
	return r.scale
}

// SetScale updates the display scale, for a window moved between displays of
// different densities. Values <= 0 are ignored.
func (r *Renderer) SetScale(s float32) {
	if s > 0 {
		r.scale = s
		if r.overlay != nil {
			r.overlay.scale = s
		}
	}
}

// ShadowsEnabled, DeferredEnabled and ShadowDistance report the current setting of
// the matching Enable*/Set* call. They exist so these toggles can be bound to
// something that has to read them back — a console variable, a settings panel —
// without the caller keeping its own shadow copy in sync.
func (r *Renderer) ShadowsEnabled() bool {
	return r.shadowsEnabled
}

func (r *Renderer) DeferredEnabled() bool {
	return r.deferredEnabled
}

func (r *Renderer) ShadowDistance() float32 {
	return r.shadowDistance
}

// StatsVisible reports whether the debug HUD is showing (see ShowFPS).
func (r *Renderer) StatsVisible() bool {
	return r.showFPS
}

// Stats returns the renderer's rolling frame-time statistics (CPU/GPU ms, FPS —
// the same numbers ShowFPS draws onscreen), for callers that want them
// programmatically (e.g. an automated before/after comparison) rather than reading
// the HUD. GPU timestamps are only recorded while ShowFPS(true) is active.
func (r *Renderer) Stats() *RendererStats {
	return r.stats
}

// ClearColor is the colour the frame is cleared to (see SetClearColor).
func (r *Renderer) ClearColor() colors.RGBA32F {
	return r.clear
}

// FontColor is the colour the debug HUD's text is drawn in.
func (r *Renderer) FontColor() colors.RGBA32F {
	return r.fontColor
}

// SetFontColor sets the colour of the debug HUD's text.
func (r *Renderer) SetFontColor(rgba colors.RGBA32F) {
	r.fontColor = rgba
}

// SetShadowDistance caps how far along the camera's view frustum directional shadows
// are fit: a smaller distance packs the shadow map's resolution into the near view for
// sharper shadows, at the cost of no shadows beyond it. Pass 0 for the automatic
// default (fit reaches the far side of the scene's bounding sphere).
// It has no effect while ShadowCascaded is using explicit Steps: the outermost step is
// where shadows stop, and one setting owning the far end is better than two.
func (r *Renderer) SetShadowDistance(distance float32) {
	r.shadowDistance = distance
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
	name := cfg.Backend
	if name == "" {
		name = os.Getenv("PIX_GPU_BACKEND")
	}
	var backend gpu.Backend
	if name != "" {
		var ok bool
		backend, ok = gpu.Lookup(name)
		if !ok {
			return nil, fmt.Errorf("render: unknown backend %q", name)
		}
	} else {
		backend = gpu.Instance(nil)
	}
	if err := backend.Init(); err != nil {
		return nil, fmt.Errorf("render: init backend: %w", err)
	}
	scale := cfg.Scale
	if scale <= 0 {
		scale = 1
	}
	r := &Renderer{
		backend:   backend,
		scale:     scale,
		clear:     colors.RGBA32F{0, 0, 0, 1},      //TODO: extract as constant
		fontColor: colors.RGBA32F{1, 0.9, 0.35, 1}, //TODO: extract as constant
		// One uploader for the process, not one per frame: its staging arena only
		// pays off by keeping its memory across frames (see uploader).
		uploader:      newUploader(backend),
		GeometryStore: geometries.NewStore(backend),
		TextureStore:  textures.NewStore(backend),
		MaterialStore: materials.NewStore(backend),
		stats:         newRendererStats(60),
	}

	switch {
	case cfg.Window != nil:
		if err := r.attachWindow(cfg.Window, cfg.Width, cfg.Height); err != nil {
			r.Destroy()
			return nil, fmt.Errorf("render: attach window: %w", err)
		}
	case cfg.Width > 0 && cfg.Height > 0:
		r.attachTexture(cfg.Width, cfg.Height)
	}
	return r, nil
}

// attachTexture configures an internally-owned RGBA8 render target of w×h.
func (r *Renderer) attachTexture(w, h uint32) {
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
	if r.depth.IsValid() {
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
	if r.diffuseTexture.IsValid() {
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
		if t.IsValid() {
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
		r.backend.DestroyPipeline(r.particleUpdatePipeline)
		r.backend.DestroyPipeline(r.shadowPipeline)
		for _, p := range r.drawPipelines {
			r.backend.DestroyPipeline(p)
		}
		for _, p := range r.lightingPipelines {
			r.backend.DestroyPipeline(p)
		}
	}
	r.cullPipeline = r.backend.CreateComputePipeline(gpu.ComputePipelineDescriptor{Shader: shaders.ForBackend(r.backend, shaders.SceneCull), Entry: "main", Label: "scene-cull"})
	r.skinPipeline = r.backend.CreateComputePipeline(gpu.ComputePipelineDescriptor{Shader: shaders.ForBackend(r.backend, shaders.SceneSkin), Entry: "main", Label: "scene-skin"})
	r.particleUpdatePipeline = r.backend.CreateComputePipeline(gpu.ComputePipelineDescriptor{Shader: shaders.ForBackend(r.backend, shaders.ParticleUpdate), Entry: "main", Label: "particle-update"})
	// Shadow depth pass: position-only vertex-pull, no color attachment, writes depth.
	// Cull is disabled so thin geometry still occludes from the light's view.
	shadowPipe := func(frag []byte, label string) gpu.Pipeline {
		return r.backend.CreateGraphicsPipeline(gpu.PipelineDescriptor{
			VertexShader: shaders.ForBackend(r.backend, shaders.SceneShadowVert), FragmentShader: shaders.ForBackend(r.backend, frag),
			Topology: gpu.TopologyTriangles, DepthFormat: gpu.FormatDepth32F,
			DepthTest: true, DepthWrite: true, DepthCompare: gpu.CompareGreater,
			CullMode: gpu.CullNone, Label: label,
		})
	}
	r.shadowPipeline = shadowPipe(shaders.SceneShadowFrag, "scene-shadow")
	for i, k := range r.drawPipelineKeys {
		r.drawPipelines[i] = r.buildDrawPipe(k)
	}
	for i, k := range r.lightingKeys {
		r.lightingPipelines[i] = r.buildLightingPipe(k.fragment)
	}
	r.pipelinesReady = true
	if r.overlay != nil {
		r.overlay.destroy()
		r.overlay = newOverlay(r.backend, r.TextureStore, r.scale, r.color, gpu.FormatDepth32F)
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
	shaderHash       uint64 // cached (vertex,fragment) identity — the dedup key
	vertex, fragment []byte // kept only to build the pipeline
	cull             materials.CullMode
	blend            materials.BlendMode
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
			VertexShader: shaders.ForBackend(r.backend, vert), FragmentShader: shaders.ForBackend(r.backend, k.fragment),
			Topology:     gpu.TopologyTriangles,
			ColorFormats: []gpu.Format{gpu.FormatRGBA8Unorm, gpu.FormatRG16F, gpu.FormatRGBA8Unorm, gpu.FormatRGBA8Unorm},
			DepthFormat:  gpu.FormatDepth32F, DepthTest: true, DepthWrite: true, DepthCompare: gpu.CompareGreater,
			CullMode: gpu.CullMode(k.cull), FrontFaceCW: true,
		})
	}
	var blend []gpu.BlendState
	switch k.blend {
	case materials.BlendAlpha:
		blend = []gpu.BlendState{{Enable: true, ColorOp: gpu.BlendFactorOp{Src: gpu.BlendSrcAlpha, Dst: gpu.BlendOneMinusSrcAlpha, Op: gpu.BlendAdd}}}
	case materials.BlendAdditive:
		blend = []gpu.BlendState{{Enable: true, ColorOp: gpu.BlendFactorOp{Src: gpu.BlendSrcAlpha, Dst: gpu.BlendOne, Op: gpu.BlendAdd}}}
	}
	// Transparent (blended) materials test depth against opaque geometry but do NOT
	// write depth, so they don't occlude each other and blend correctly.
	depthWrite := k.blend == materials.BlendOpaque
	return r.backend.CreateGraphicsPipeline(gpu.PipelineDescriptor{
		VertexShader: shaders.ForBackend(r.backend, vert), FragmentShader: shaders.ForBackend(r.backend, k.fragment),
		Topology: gpu.TopologyTriangles, ColorFormats: []gpu.Format{r.color},
		DepthFormat: gpu.FormatDepth32F, DepthTest: true, DepthWrite: depthWrite, DepthCompare: gpu.CompareGreater,
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
// The G-buffer's depth is bound read-only and tested with CompareLess against the
// fullscreen triangle's depth of 0 (see fullscreen.vert), so the hardware rejects
// background pixels — those still holding the 0 clear value — before the fragment
// shader runs at all. Under reversed-Z the background is 0 and geometry is greater,
// so the sense is the mirror of the conventional arrangement. Without it every pixel
// in the viewport pays for an invocation and a depth sample just to discard.
func (r *Renderer) buildLightingPipe(fragment []byte) gpu.Pipeline {
	return r.backend.CreateGraphicsPipeline(gpu.PipelineDescriptor{
		VertexShader: shaders.ForBackend(r.backend, shaders.FullscreenVert), FragmentShader: shaders.ForBackend(r.backend, fragment),
		Topology: gpu.TopologyTriangles, ColorFormats: []gpu.Format{r.color},
		DepthFormat: gpu.FormatDepth32F, DepthTest: true, DepthWrite: false, DepthCompare: gpu.CompareLess,
		CullMode: gpu.CullNone,
	})
}

// lightingPipelineKey identifies one deferred shading model: the hash of its Lighting()
// shader (the dedup key) plus the SPIR-V itself, kept only to rebuild the pipeline on a
// format change — the same shape as materialPipeline.
type lightingPipelineKey struct {
	hash     uint64
	fragment []byte
}

// lightingPipelineFor resolves a material's Lighting() shader to a lighting-pipeline
// index, building and caching it the first time a given shader is seen (one fullscreen
// pipeline per unique shading model, shared by every material using that Lighting()).
//
// This deliberately keys on the shader rather than on Material.Hash: two different
// material TYPES that share a shading model must share one fullscreen pass, and their
// Hashes differ (each covers its own forward/deferred shaders too). Identity is a
// slice-header compare first — every built-in hands over the same //go:embed slice on
// every call, so that hits without reading any SPIR-V — falling back to a byte hash
// only for a distinct slice, which is then cached on the key.
func (r *Renderer) lightingPipelineFor(lightingShader []byte) uint32 {
	for i := range r.lightingKeys {
		if materials.SameSPIRV(r.lightingKeys[i].fragment, lightingShader) {
			return uint32(i)
		}
	}
	hash := materials.HashBytes(lightingShader)
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

// renderState is everything the renderer keeps on the GPU for one packet source,
// derived entirely from the packets that source has published.
type renderState struct {
	dl *drawList
	// lights is the GPU light table this source's packets are compiled into, and
	// shadows the depth-map resources of its casting lights, keyed by light identity.
	// Both are derived from packets; neither is anything a producer can see.
	lights  *Lights
	shadows map[scenes.LightID]*shadowResource
	// joints is the GPU palette buffer compute skinning reads, uploaded from the
	// packet's Joints table each frame. Host-visible: the whole table is rewritten
	// every frame anyway, so staging it would buy nothing.
	joints    gpu.Buffer
	jointsCap uint32
	// particles is the GPU simulation state of this source's particle systems.
	particles map[scenes.ParticleID]*particleState
}

// syncJoints uploads a packet's joint palettes, growing the buffer as needed. Returns
// the device address the skinning dispatches read from.
func (st *renderState) syncJoints(backend gpu.Backend, joints []glm.Mat4f) uint64 {
	n := uint32(len(joints))
	if n == 0 {
		return st.joints.Addr
	}
	if n > st.jointsCap {
		if st.joints.IsValid() {
			backend.Free(st.joints)
		}
		st.jointsCap = max(n*2, 1)
		st.joints = backend.Alloc(uint64(st.jointsCap)*64, gpu.MemoryHost, "joints")
	}
	st.joints.Write(utils.ToBytesSlice(joints), 0)
	return st.joints.Addr
}

// stateFor returns the GPU state for a source, creating it on first sight. First use
// and structural growth allocate; steady-state frames reuse everything.
func (r *Renderer) stateFor(id scenes.SourceID) *renderState {
	if st, ok := r.sources[id]; ok {
		return st
	}
	if r.sources == nil {
		r.sources = make(map[scenes.SourceID]*renderState)
	}
	st := &renderState{dl: newDrawList(r.backend), lights: NewLights(r.backend)}
	r.sources[id] = st
	return st
}

// ReleaseSource drops the GPU state cached for a producer. Call it when a Scene is
// destroyed, passing Scene.ID: scene teardown does not reach into a renderer to do
// this, so nothing else will. Releasing a source that is used again is safe — it
// initializes from the next packet's complete tables — but it throws away work.
//
// Renderer.Destroy releases every remaining source, so a program that tears the
// renderer down at exit need not track this.
func (r *Renderer) ReleaseSource(id scenes.SourceID) {
	st, ok := r.sources[id]
	if !ok {
		return
	}
	st.dl.destroy()
	st.lights.Destroy()
	if st.joints.IsValid() {
		r.backend.Free(st.joints)
	}
	for _, ps := range st.particles {
		ps.destroy(r.backend)
	}
	for _, sh := range st.shadows {
		sh.destroy()
	}
	delete(r.sources, id)
}

// pipelineUnresolved marks a cell of a pool's pipeline table that has not been built
// yet. Zero is a perfectly good pipeline index, so it cannot double as "empty".
const pipelineUnresolved uint32 = 0xFFFFFFFF

// poolPipelines is every draw pipeline the instances of one material pool can select
// between. A pool is keyed by a Shader that never changes for its lifetime, so the
// only things that vary within it are the per-instance cull and blend modes, plus the
// pass the renderer picks. Cull and blend have three values each and there are two
// passes, so the entire space is a fixed table indexed directly — no hashing, no
// scan, no map — filled lazily because most pools use one or two of the cells.
type poolPipelines struct {
	table [2][3][3]uint32 // [pass][cull][blend]
}

func newPoolPipelines() poolPipelines {
	var pp poolPipelines
	for p := range pp.table {
		for c := range pp.table[p] {
			for b := range pp.table[p][c] {
				pp.table[p][c][b] = pipelineUnresolved
			}
		}
	}
	return pp
}

// pipelineForPool resolves the draw pipeline for one material of a pool, given that
// material's rasterization state. When deferred rendering is enabled
// (EnableDeferredRendering), an opaque material whose pool supplies both Deferred and
// Lighting renders through the G-buffer; everything else (blended, deferred disabled,
// or a pool missing either shader) renders forward. Blended materials are forced
// forward: the G-buffer holds one surface per pixel, so it cannot represent a fragment
// that composites over what is behind it.
//
// Eligibility is asked of the pool rather than the material because the pool is keyed
// by those very shaders — a material cannot disagree with it, and so cannot ask for a
// G-buffer pipeline built out of SPIR-V its pool does not have.
func (r *Renderer) pipelineForPool(p *materials.Pool, cull materials.CullMode, blend materials.BlendMode) uint32 {
	for uint32(len(r.pools)) <= p.Index() {
		r.pools = append(r.pools, newPoolPipelines())
	}
	pp := &r.pools[p.Index()]

	sh := p.Shader()
	ps := passForward
	if r.deferredEnabled && blend == materials.BlendOpaque && sh.Deferred != nil && sh.Lighting != nil {
		ps = passGBuffer
	}
	// The toggle is part of the index, not a reason to invalidate: flipping deferred
	// rendering selects the other row and finds whatever was already built there.
	if id := pp.table[ps][cull][blend]; id != pipelineUnresolved {
		return id
	}

	key := materialPipeline{
		pass: passForward, shaderHash: p.Hash(), vertex: sh.Vertex, fragment: sh.Forward,
		cull: cull, blend: blend,
	}
	if ps == passGBuffer {
		key = materialPipeline{
			pass: passGBuffer, shaderHash: p.Hash(), vertex: sh.Vertex, fragment: sh.Deferred,
			cull: cull, lightingIdx: r.lightingPipelineFor(sh.Lighting),
		}
	}
	id := r.pipelineFor(key)
	pp.table[ps][cull][blend] = id
	return id
}

// pipelineForMaterial resolves the draw pipeline for a material. Everything it needs
// lives in the material's pool; the material itself only says which pool and which
// slot within it.
func (r *Renderer) pipelineForMaterial(m materials.Material) uint32 {
	p, id := m.Pool(), m.ID()
	return r.pipelineForPool(p, p.Cull(id.Slot), p.Blend(id.Slot))
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

// The named material constructors (NewBasicMaterial, NewBlinnPhongMaterial,
// NewPBRMaterial, NewRawMaterial) are shortcuts onto MaterialStore — see materials.go.

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
func (r *Renderer) SetClearColor(rgba colors.RGBA32F) {
	r.clear = rgba
}

// ShowFPS toggles the debug HUD (FPS + CPU + GPU frame times, in the bitmap font).
func (r *Renderer) ShowFPS(on bool) {
	r.showFPS = on
	if on {
		r.ensureOverlay()
		if !r.gpuPool.IsValid() {
			r.gpuPool = r.backend.CreateTimestampPool(2)
		}
	}
}

// ensureOverlay creates the shared debug overlay if a consumer (the FPS HUD or the
// console) needs it and the pipelines it depends on exist yet. It is deferred rather
// than created up front because a renderer that never shows either should not pay for
// the pipeline.
func (r *Renderer) ensureOverlay() {
	if r.overlay == nil && r.pipelinesReady {
		r.overlay = newOverlay(r.backend, r.TextureStore, r.scale, r.color, gpu.FormatDepth32F)
	}
}

// overlayActive reports whether anything wants the overlay drawn this frame.
func (r *Renderer) overlayActive() bool {
	return r.showFPS || r.IsConsoleOpen()
}

// IsConsoleOpen reports whether the console is enabled and currently open. It is the
// check an application makes to stand its own input down:
//
//	if !r.IsConsoleOpen() {
//	    controls.Update()
//	}
//
// The console captures typed text but cannot stop anything else from polling the same
// keys, so without this the camera flies around while the user types `set`. Safe on a
// renderer that never enabled a console — it is simply false, so a caller need not
// know whether one exists.
func (r *Renderer) IsConsoleOpen() bool {
	return r.console != nil && r.console.Visible()
}

// EnableConsole turns on the developer console, reading from in, and returns it so
// values can be registered:
//
//	c := r.EnableConsole(gamekitinput.New(win))
//	console.Bind(c, "shadow.distance", &shadowDistance, "directional shadow fit")
//
// The console starts hidden; the user opens it with its toggle key (` by default).
// The renderer's own switches (shadows, deferred, stats, the clear and HUD colours,
// and the read-only size/aspect) are registered automatically, so `list` is useful
// before the application binds anything of its own.
//
// Calling this again returns the existing console rather than replacing it, so the
// registrations survive.
//
// The application is responsible for not acting on input the console is consuming:
// check IsConsoleOpen before running camera controls, or the camera will fly around
// while the user types. The renderer drives Console.Update itself, one frame
// before that check is read; an application that wants the toggle to take effect
// within the same frame can call Update itself first — the renderer's later call then
// finds the buffers already drained and does nothing.
func (r *Renderer) EnableConsole(in console.Input) *console.Console {
	if r.console == nil {
		r.console = console.New(in)
		r.registerBuiltins(r.console)
		r.registerCommands(r.console)
	}
	r.ensureOverlay()
	return r.console
}

// DisableConsole detaches the console; its registrations are dropped with it.
func (r *Renderer) DisableConsole() {
	r.console = nil
}

// Console returns the console, or nil if EnableConsole was never called.
func (r *Renderer) Console() *console.Console {
	return r.console
}

// Render draws the scene from cam into the configured target (swapchain or texture).
// The camera is not retained.
func (r *Renderer) Render(scene scenes.Producer, cam Camera) {
	//TODO: r.swapchain.H == 0 is a bit of a smell something like r.swapchain.IsValid()
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
	planes := glm.FrustumPlanes(vp)
	drawVP := flipClipY(vp)
	eye := cam.Position()
	if r.console != nil {
		r.console.Update()
	}
	if r.overlayActive() {
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
	// Recorded after everything is drawn but before the frame is submitted, so a
	// windowed capture reads the image while it is still ours (see screenshot.go).
	// encode has usually done it already, just before drawing the overlay; this covers
	// the frames with no overlay to exclude, and recordScreenshot only copies once.
	r.recordScreenshot(cmd, target)
	cpu += time.Since(encStart)
	r.stats.AddCPUTime(cpu)

	if r.hasTarget {
		// Offscreen: submit the frame ourselves (windowed rendering submits via Present).
		// Capture's readback submits after this on the same queue, so it sees the result.
		r.backend.Wait(r.backend.Submit(cmd))
	} else {
		r.backend.Present(r.swapchain, cmd)
	}

	// The frame is submitted, so this packet's simulation step has happened: let the
	// producer retire it. Doing it here, after submission, is what makes the step
	// exactly-once without an acknowledgement protocol — extraction only ever reads,
	// and one function owns the order of the two halves.
	scene.Rendered()

	// Both paths above drain the queue, so the readback buffer holds finished pixels.
	r.writeScreenshot()

	r.readGPU()
	r.stats.EndFrame()
}

// Capture copies the current render target into a CPU buffer and returns it as RGBA8
// (headless targets only; the target needs transfer usage). Call after Render.
func (r *Renderer) Capture() []byte {
	if !r.hasTarget {
		return nil
	}
	n := int(r.width * r.height * 4)
	if !r.readback.IsValid() {
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
func (r *Renderer) Pixels() []byte {
	return r.Capture()
}

// encode records the frame: the shadow views (cull + depth pass per casting light),
// then the main view (cull + lit draw), plus GPU timestamps + the HUD when enabled.
// All compute culls run first (outside any render pass), share one barrier, then the
// shadow depth passes and the main color pass consume their results.
func (r *Renderer) encode(cmd gpu.CommandBuffer, target gpu.Texture, scene scenes.Producer, planes [6]glm.Vec4f, drawVP glm.Mat4f, eye glm.Vec3f) {
	if r.showFPS && r.gpuPool.IsValid() {
		cmd.ResetTimestamps(r.gpuPool, 2)
		cmd.WriteTimestamp(r.gpuPool, 0, gpu.StageNone)
	}
	st := r.stateFor(scene.ID())
	dl := st.dl
	// Shadow views only make sense when there's geometry to cast; with an empty draw
	// list the per-view buffers would be zero-sized (visCap == 0) and there's nothing
	// to render into a depth map anyway, so skip all shadow work.
	var views []shadowView
	if dl.batchCount() > 0 {
		views = r.collectShadowViews(st)
		if len(views) > 0 {
			dl.ensureShadowViews(len(views))
		}
	}

	// 1. Compute pre-skinning, then cull every view (shadow views filter to
	// casters), then simulate particles. Skinning writes positions and particle
	// update writes particle records that the shadow/G-buffer/forward vertex stages
	// read (not cull — it only reads CPU-supplied bounds), so one barrier after all
	// three covers everything.
	hasParticles := len(r.frame.Particles.Data) > 0
	if dl.batchCount() > 0 || hasParticles {
		if cmds := r.skinCommands(&r.frame); len(cmds) > 0 {
			r.dispatchSkinning(cmd, dl, cmds, st.syncJoints(r.backend, r.frame.Joints.Data))
		}
		for i := range views {
			v := &dl.shadowViews[i]
			sp := glm.FrustumPlanes(views[i].cam.ViewProjection())
			r.cullInto(cmd, dl, v.indirectBuf, v.visibleBuf, sp, 1, eye)
		}
		if dl.batchCount() > 0 {
			r.cullInto(cmd, dl, dl.indirectBuf, dl.visibleBuf, planes, 0, eye)
		}
		if hasParticles {
			r.dispatchParticleUpdate(cmd, st, &r.frame)
		}
		cmd.Barrier(gpu.StageCompute, gpu.StageIndirect|gpu.StageVertex, 0)
	}

	// 2. Shadow depth passes: fill each view's map, then hand it to the samplers.
	for i := range views {
		r.recordShadowDepth(cmd, dl, &dl.shadowViews[i], views[i])
		//FIXME: use VK_KHR_unified_image_layouts
		// this should remove the PreparedSampled from the RHI API
		cmd.PrepareSampled(r.TextureStore.GPU(views[i].m), gpu.StageFragment)
	}

	// 3. Every run's drawRoot is filled once regardless of pass (same camera either
	// way); issueDraws below then filters by pass to route it into the right render
	// pass with the right pipeline set.
	r.fillDrawRoots(dl, drawVP, eye, st.lights.Addr(), r.frame.Time)
	gbufferActive := r.hasGBufferRuns(dl)
	// A debug view needs the G-buffer to have actually been filled; with nothing
	// rendering deferred there is nothing to show, so the frame shades normally.
	debugShown := gbufferActive && r.debugViewActive()
	// Object/triangle id views are a separate, self-contained geometry pass (see
	// recordDebugIDView) — they replace the G-buffer fill + lighting entirely,
	// regardless of whether anything would have gone through the deferred path this
	// frame at all.
	idShown := r.idViewActive()

	// 4. G-buffer fill + deferred lighting, only when something renders that way —
	// skipped entirely in favor of the id pass below when one is active.
	if idShown {
		r.recordDebugIDView(cmd, dl, target, drawVP)
	} else if gbufferActive {
		r.ensureGBuffer()
		cmd.BeginRenderPass(gpu.RenderTargets{
			Color: []gpu.ColorAttachment{
				{Texture: r.diffuseTexture, Load: gpu.LoadClear, Store: gpu.StoreKeep},
				{Texture: r.normalTexture, Load: gpu.LoadClear, Store: gpu.StoreKeep},
				{Texture: r.materialTexture, Load: gpu.LoadClear, Store: gpu.StoreKeep},
				{Texture: r.emissiveTexture, Load: gpu.LoadClear, Store: gpu.StoreKeep},
			},
			Depth: &gpu.DepthAttachment{Texture: r.depth, Load: gpu.LoadClear, Store: gpu.StoreKeep, Clear: 0.0},
		})
		cmd.SetViewport(0, 0, float32(r.width), float32(r.height), 0, 1)
		cmd.SetScissor(0, 0, int32(r.width), int32(r.height))
		r.issueDraws(cmd, dl, passGBuffer)
		cmd.EndRenderPass()

		cmd.PrepareSampled(r.diffuseTexture, gpu.StageFragment)
		cmd.PrepareSampled(r.normalTexture, gpu.StageFragment)
		cmd.PrepareSampled(r.materialTexture, gpu.StageFragment)
		cmd.PrepareSampled(r.emissiveTexture, gpu.StageFragment)
		cmd.PrepareSampled(r.depth, gpu.StageFragment)
		if debugShown {
			// Show one G-buffer target instead of shading it. The geometry pass above
			// ran either way, so toggling a view changes only what reaches the screen.
			//
			// This REPLACES the lighting pass but must not short-circuit the rest of
			// encode: the forward pass below still has to run for the overlay, and the
			// closing GPU timestamp still has to be written. Returning early here reset
			// a timestamp query and never wrote it, and the next frame's blocking read
			// hung the process.
			r.ensureGBufferSampler()
			r.recordDebugView(cmd, dl, target, drawVP, eye, st.lights.Addr())
		} else {
			r.recordLighting(cmd, dl, target, drawVP, eye, st.lights.Addr())
		}
	}

	// 5. Forward pass: transparents, and any Opaque material that did not
	// qualify for deferred. If the G-buffer ran, its color+depth are loaded (not
	// cleared) so forward geometry composites over the already-lit scene and depth-
	// tests correctly against it; otherwise this is the only pass, so it clears.
	colorLoad, depthLoad := gpu.LoadClear, gpu.LoadClear
	if gbufferActive || idShown {
		colorLoad, depthLoad = gpu.LoadKeep, gpu.LoadKeep
	}
	// Forward geometry is suppressed while a G-buffer view or an id view is up: the
	// point is to see that target on its own, not transparents composited over it.
	drawForward := !debugShown && !idShown

	// The pass itself still runs even when drawForward is false — it is what draws
	// the overlay and closes the frame.
	cmd.BeginRenderPass(gpu.RenderTargets{
		Color: []gpu.ColorAttachment{{Texture: target, Load: colorLoad, Store: gpu.StoreKeep, Clear: r.clear}},
		Depth: &gpu.DepthAttachment{Texture: r.depth, Load: depthLoad, Store: gpu.StoreKeep, Clear: 0.0},
	})
	cmd.SetViewport(0, 0, float32(r.width), float32(r.height), 0, 1)
	cmd.SetScissor(0, 0, int32(r.width), int32(r.height))
	if drawForward {
		r.issueDraws(cmd, dl, passForward)
		if hasParticles {
			r.drawParticles(cmd, st, &r.frame, drawVP, eye)
		}
	}
	if r.overlayActive() && r.overlay != nil {
		// A capture is of the SCENE, not of the tools used to inspect it, so the copy
		// goes in before the overlay. That means breaking the pass, since a copy cannot
		// happen inside one — only on frames that actually capture, and only when there
		// is an overlay to exclude.
		if r.pendingShot != nil {
			cmd.EndRenderPass()
			r.recordScreenshot(cmd, target)
			cmd.BeginRenderPass(gpu.RenderTargets{
				Color: []gpu.ColorAttachment{{Texture: target, Load: gpu.LoadKeep, Store: gpu.StoreKeep}},
				Depth: &gpu.DepthAttachment{Texture: r.depth, Load: gpu.LoadKeep, Store: gpu.StoreKeep},
			})
			cmd.SetViewport(0, 0, float32(r.width), float32(r.height), 0, 1)
			cmd.SetScissor(0, 0, int32(r.width), int32(r.height))
		}
		r.overlay.draw(cmd, float32(r.width), float32(r.height))
	}
	cmd.EndRenderPass()
	if r.showFPS && r.gpuPool.IsValid() {
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
	cam           Camera
	m             textures.Texture
	width, height uint32
	// x is where this view's square starts along the map's width, which is how cascades
	// share one texture (see collectShadowViews).
	x int32
	// clear reports whether this view is the one that clears the map. Views sharing a
	// texture must agree that exactly one does, or each would wipe what the others drew.
	clear bool
}

// collectShadowViews gathers every shadow-map render request the renderer has
// resources for: one per casting directional/spot light and six per casting point
// light. Empty when shadows are disabled or nothing casts.
func (r *Renderer) collectShadowViews(st *renderState) []shadowView {
	if !r.shadowsEnabled {
		return nil
	}
	var out []shadowView
	for _, sh := range st.shadows {
		switch {
		case !sh.m.IsValid():
		case len(sh.cascades) > 0:
			// Every cascade renders into the same texture, so exactly one of them may
			// clear it and the rest must load what the others wrote.
			side := sh.size()
			for i, c := range sh.cascades {
				out = append(out, shadowView{
					cam: c.cam, m: sh.m, x: int32(i) * int32(side),
					width: side, height: side, clear: i == 0,
				})
			}
		default:
			out = append(out, shadowView{
				cam: sh.cam, m: sh.m, width: sh.width, height: sh.height, clear: true,
			})
		}
		for i := range sh.faces {
			if f := sh.faces[i]; f.m.IsValid() {
				out = append(out, shadowView{cam: f.cam, m: f.m, width: sh.width, height: sh.height, clear: true})
			}
		}
	}
	return out
}

// syncScene uploads the renderer's shared tables + the scene's per-scene GPU state.
// It resolves each mesh's draw pipeline from its material (shaders + raster) every
// frame and rebuilds the draw list when the mesh set OR any pipeline assignment
// changed (so a material raster/blend change re-batches without an explicit dirty).
func (r *Renderer) syncScene(scene scenes.Producer, cam Camera, cmd gpu.CommandBuffer) {
	// Device-only resources (geometry streams + descriptors, material records) are
	// staged through the shared uploader, which records the copies into the head of
	// this frame's command buffer and barriers them against the passes that read
	// them — no separate submit. The scene owns transforms, skinning, its draw list
	// and lights, and writes those straight to MemoryHost buffers (see Scene.Sync),
	// so a frame where only scene-owned state changed (a refit shadow camera, an
	// animated pose) stages nothing at all and End skips even the barrier.
	// The light table encodes each light's shadow map for the shader, so the scene
	// needs the global toggle before it rebuilds that table — otherwise disabling
	// shadows only stops refreshing the maps and the shader samples the last one.
	st := r.stateFor(scene.ID())

	// Scene.Sync settles world transforms, skinning and the clock; extraction publishes
	// the result. Both happen exactly once per frame and before anything reads the
	// packet — extracting twice would hand the second call a packet whose per-frame
	// flags (TransformsDirty) the first had already consumed.
	scene.Extract(&r.frame)

	if r.shadowsEnabled {
		r.prepareShadows(st, &r.frame, cam)
	}
	up := r.uploader
	up.Begin(cmd)
	r.GeometryStore.Sync(up)
	r.MaterialStore.Sync(up)
	up.End(cmd)

	// The light table is the renderer's buffer, packed from the packet's light values
	// and the shadow resources prepared above. The producer never sees a map index.
	st.lights.rebuild(r.frame.Environment, r.frame.Lights.Data, st.shadows, r.shadowsEnabled, r.shadowFilter)
	st.lights.Sync()

	r.syncDrawList(scene)
}

// prepareFrom runs one frame's producer-side work — settle the scene, extract a
// packet, refresh the draw list — without recording any GPU commands. Render does the
// same steps as part of a frame; this is the seam tests use to inspect the derived
// state, and it keeps them from having to know that extraction happens exactly once.
func (r *Renderer) prepareFrom(scene scenes.Producer) {
	scene.Extract(&r.frame)
	r.syncDrawList(scene)
}

// syncDrawList refreshes the scene's batches for this frame. The drawables and the
// distinct set of materials they reference are collected only on a structural change
// (drawableDirty); the pipeline ids are re-resolved every frame off that cached
// material set, because a material can change pipeline — a blend-mode or cull change —
// without the draw list structure moving at all.
//
// The work here is proportional to the number of distinct MATERIALS, not drawables.
// A scene with ten thousand objects sharing six materials resolves six pipelines and
// compares six ids; only when one of those six actually changes does anything touch
// the per-drawable arrays. Resolving off the collected set is also what keeps pipeBuf
// aligned with the drawables — both come from the one collectDrawables walk, so no
// second traversal has to reproduce its ordering and its flagAttached filtering.
func (r *Renderer) syncDrawList(scene scenes.Producer) {
	dl := r.stateFor(scene.ID()).dl
	p := &r.frame

	// The transform buffer is the renderer's, so the renderer fills it. The producer
	// only says whether the matrices moved; a frame where nothing did writes nothing.
	//
	// A state seeing its first packet uploads regardless: the dirty flag reports change
	// since the last EXTRACTION, which a cache created just now never saw. Without this
	// a source released and then drawn again would render from an unwritten buffer,
	// because the producer has no idea its consumer went away.
	if p.TransformsDirty || !dl.worldBuf.IsValid() {
		dl.sync(p.Transforms.Data, p.InstanceTransforms.Data)
	}

	// Resolve one pipeline per DISTINCT material, reading everything from the pool the
	// material's identity names. This is the only place the packet's resource
	// identities are turned back into renderer objects.
	dl.matPipe = dl.matPipe[:0]
	dl.matPool = dl.matPool[:0]
	dl.matBlend = dl.matBlend[:0]
	for _, id := range p.Materials.Data {
		pool := r.MaterialStore.PoolAt(id.Pool)
		blend := pool.Blend(id.Slot)
		dl.matPool = append(dl.matPool, pool)
		dl.matBlend = append(dl.matBlend, blend)
		dl.matPipe = append(dl.matPipe, r.pipelineForPool(pool, pool.Cull(id.Slot), blend))
	}
	if dl.builtRevision == p.Meshes.Revision && slices.Equal(dl.matPipe, dl.batchedMatPipe) {
		return
	}
	dl.expand(p)
	dl.rebuild(r.GeometryStore)
	dl.builtRevision = p.Meshes.Revision
	dl.batchedMatPipe = append(dl.batchedMatPipe[:0], dl.matPipe...)
}

// prepareShadows gives every casting light in the packet the resources it asked for —
// a depth map, a camera aimed or fitted for this frame — and retires the resources of
// lights that stopped casting. Runs before the light table is packed, so the table can
// carry each light's map index and view-projection.
//
// The packet says only which lights cast and at what resolution. Everything here —
// which projection, where it points, how it is fitted to the view, what the depth bias
// works out to in normalized units — is the renderer's, because all of it depends on
// the view being rendered and on caster bounds the producer does not track.
func (r *Renderer) prepareShadows(st *renderState, p *scenes.FramePacket, cam Camera) {
	if r.shadowSampler.H == 0 {
		r.shadowSampler = r.backend.CreateSampler(gpu.SamplerDescriptor{
			MinLinear: true, MagLinear: true,
			AddressU: gpu.AddressClamp, AddressV: gpu.AddressClamp,
			Compare: gpu.CompareGreaterEqual, // PCF comparison sampler (reversed-Z: nearer is greater)
			Label:   "shadow-cmp",
		})
	}

	// Point lights first: they are aimed from the light alone and need no scene bounds.
	var needsFit bool
	for _, l := range p.Lights.Data {
		if !l.CastsShadow {
			continue
		}
		if l.Kind != scenes.LightPoint {
			needsFit = true
			continue
		}
		sh := st.shadowFor(l.ID)
		sh.seen = true
		r.aimPointShadow(sh, l)
	}
	if !needsFit {
		st.retireUnseenShadows()
		return
	}

	center, radius := casterBounds(p)
	fit := r.shadowFitFor(cam, center, radius, r.shadowReach())
	for _, l := range p.Lights.Data {
		if !l.CastsShadow || l.Kind == scenes.LightPoint {
			continue
		}
		sh := st.shadowFor(l.ID)
		sh.seen = true
		switch l.Kind {
		case scenes.LightDirectional:
			// The map has to exist before the fit: every algorithm derives its depth
			// bias from how much world space one of its texels covers. Its shape is the
			// algorithm's to choose.
			// One square per slice, laid out along the width: four 1024 cascades is a
			// single 4096x1024 texture, each rendered through its own scissor. A single
			// fit reports one level, so this is its natural size.
			width, height := cascadeAtlas(requestedSize(l), uint32(r.Shadows().levels()))
			sh.ensureMap(r.TextureStore, width, height)
			r.fitDirectional(sh, l, fit)
		case scenes.LightSpot:
			sh.ensurePerspective(l.Angle, l.Range)
			sh.ensureMap(r.TextureStore, requestedSize(l), requestedSize(l))
			aimSpotShadow(sh, l)
		}
	}
	st.retireUnseenShadows()
}

// casterBounds is the world-space sphere enclosing everything the packet draws, which
// is what a directional shadow falls back to when the view frustum slice is larger than
// the scene. Derived from the packet rather than asked of the producer: the renderer
// already holds the bounds and the transforms, and a custom producer should not have to
// implement a bounding-volume query to get shadows.
//
// Recomputed per frame for now. The specification calls for caching this and
// invalidating it on caster changes; that is worth doing when it measures, not before.
func casterBounds(p *scenes.FramePacket) (glm.Vec3f, float32) {
	var lo, hi glm.Vec3f
	first := true
	reach := func(t uint32, b glm.Sphere) {
		m := transformAt(p, t)
		c := m.Mul4x1(glm.Vec4f{b.Center[0], b.Center[1], b.Center[2], 1})
		// A world matrix may scale, so the radius scales with the largest axis.
		sx := glm.Vec3f{m[0], m[1], m[2]}.Length()
		sy := glm.Vec3f{m[4], m[5], m[6]}.Length()
		sz := glm.Vec3f{m[8], m[9], m[10]}.Length()
		rr := b.Radius * max(sx, max(sy, sz))
		cmin := glm.Vec3f{c[0] - rr, c[1] - rr, c[2] - rr}
		cmax := glm.Vec3f{c[0] + rr, c[1] + rr, c[2] + rr}
		if first {
			lo, hi, first = cmin, cmax, false
			return
		}
		lo = glm.Vec3f{min(lo[0], cmin[0]), min(lo[1], cmin[1]), min(lo[2], cmin[2])}
		hi = glm.Vec3f{max(hi[0], cmax[0]), max(hi[1], cmax[1]), max(hi[2], cmax[2])}
	}
	for _, m := range p.Meshes.Data {
		for j := uint32(0); j < m.Transforms.Count; j++ {
			reach(m.Transforms.First+j, m.Bounds)
		}
	}
	if first {
		return glm.Vec3f{}, 0
	}
	center := lo.Add(hi).Scale(0.5)
	return center, hi.Sub(center).Length()
}

// transformAt resolves a packet transform index, which addresses the node matrices
// followed by the instance matrices as one array (see FramePacket.InstanceTransforms).
func transformAt(p *scenes.FramePacket, i uint32) glm.Mat4f {
	n := uint32(len(p.Transforms.Data))
	if i < n {
		return p.Transforms.Data[i]
	}
	if k := i - n; k < uint32(len(p.InstanceTransforms.Data)) {
		return p.InstanceTransforms.Data[k]
	}
	return glm.Mat4Identity[float32]()
}

// aimPointShadow allocates (once) and re-aims a point light's six cube-face shadow
// cameras from the light's current position and range.
func (r *Renderer) aimPointShadow(s *shadowResource, l scenes.LightPacket) {
	s.ensureFaceMaps(r.TextureStore, requestedSize(l))
	s.updateLocalBias(l.Range, l.ShadowBias)
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
func aimSpotShadow(s *shadowResource, l scenes.LightPacket) {
	c, ok := s.cam.(interface {
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
	_, up := lightBasis(d)
	c.SetPosition(l.Position)
	c.SetTarget(l.Position.Add(d))
	c.SetUp(up)
	c.SetFOV(glm.ToDegrees(2 * l.Angle))
	c.SetFar(l.Range)
	s.updateLocalBias(l.Range, l.ShadowBias)
}

// frustumCornersWorld returns the 8 world-space corners of the frustum described by
// viewProj (indices 0..3 near, 4..7 far). NDC is Vulkan's xy in [-1,1] with z in
// [0,1] — but the engine uses REVERSED-Z, so the near plane is at z=1 and the far
// plane at z=0 (see glm.PerspectiveRevZRH). Hence the {1, 0} order: it keeps the
// documented "near first" layout that fitDirectionalShadow depends on to measure
// its near→far slice. Reading these in the wrong order silently mis-sizes and
// mis-places the shadow box rather than failing outright.
func frustumCornersWorld(viewProj glm.Mat4f) [8]glm.Vec3f {
	inv := viewProj.Inv()
	var c [8]glm.Vec3f
	i := 0
	for _, z := range [2]float32{1, 0} {
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

// cullInto dispatches the frustum-cull compute into one view's indirect + visible
// buffers: it resets the indirect args from the shared template (zeroing each batch's
// instanceCount), points a cull root at this view's buffers (the drawable/world/region
// tables are shared), and dispatches one thread per instance. castersOnly=1 restricts
// the view to shadow casters. eye is the camera's world position, used only by LOD
// distance tests (see scene_cull.comp's selectLevel) — every view (main + shadow) uses
// the same main-camera eye, not the shadow light's own position, since LOD is a
// main-camera-relative decision regardless of which view is culling this frame. No
// barrier — the caller batches all views behind one.
func (r *Renderer) cullInto(cmd gpu.CommandBuffer, dl *drawList, indirect, visible gpu.Buffer, planes [6]glm.Vec4f, castersOnly uint32, eye glm.Vec3f) {
	indirect.Write(utils.ToBytesSlice(dl.template), 0)
	cr := cullRoot{
		drawables: dl.drawableBuf.Addr, models: dl.worldBuf.Addr, indirect: indirect.Addr,
		regions: dl.regionBuf.Addr, visible: visible.Addr,
		lods: dl.lodTableBuf.Addr, prevLevel: dl.prevLevelBuf.Addr,
		eye: glm.Vec4f{eye[0], eye[1], eye[2], 1}, count: dl.numInst,
		castersOnly: castersOnly, planes: planes,
	}
	cmd.SetPipeline(r.cullPipeline)
	cmd.Dispatch(utils.ToBytes(&cr), (dl.numInst+63)/64, 1, 1)
}

// skinCommands builds this frame's compute-skinning dispatch list, one entry per
// SkinnedMesh, into a scratch slice reused across frames. The scene supplies the
// state (which meshes are skinned, their source/output geometry, their skeleton's
// joint range); deciding what GPU work that implies is the renderer's job, like
// every other command list it builds here.
//
// Every skinned mesh is dispatched regardless of visibility — a v1 simplification;
// a scene with many off-screen skinned meshes pays for all of them.
func (r *Renderer) skinCommands(p *scenes.FramePacket) []skinCmd {
	r.skinScratch = r.skinScratch[:0]
	for _, sp := range p.Skins.Data {
		r.skinScratch = append(r.skinScratch, skinCmd{
			srcDesc: sp.Source.Slot, dstDesc: sp.Output.Slot,
			jointBase: sp.Joints.First, vertexCount: sp.VertexCount,
		})
	}
	return r.skinScratch
}

// dispatchSkinning fills one skinRoot per skinned mesh (position/attribute/skin/
// descriptor streams and the scene's joint buffer are frame-global; only
// srcDesc/dstDesc/jointBase/vertexCount vary per mesh) and issues one Dispatch per
// mesh, sized to its vertex count. See scene_skin.comp.
func (r *Renderer) dispatchSkinning(cmd gpu.CommandBuffer, dl *drawList, cmds []skinCmd, jointsAddr uint64) {
	posAddr, attrAddr := r.GeometryStore.PositionsAddr(), r.GeometryStore.AttributesAddr()
	skinAddr, descAddr := r.GeometryStore.SkinAddr(), r.GeometryStore.DescriptorsAddr()
	cmd.SetPipeline(r.skinPipeline)
	for _, c := range cmds {
		sr := skinRoot{
			pos: posAddr, attr: attrAddr, skin: skinAddr, descs: descAddr, joints: jointsAddr,
			srcDesc: c.srcDesc, dstDesc: c.dstDesc, jointBase: c.jointBase, vertexCount: c.vertexCount,
		}
		cmd.Dispatch(utils.ToBytes(&sr), (c.vertexCount+63)/64, 1, 1)
	}
}

// dispatchParticleUpdate runs every attached particle container's simulation kernel:
// stages this frame's newborns into a host-visible scratch buffer, resets the
// container's indirect-draw instance count to 0, and dispatches particle_update.comp
// over capacity+pendingCount threads (see that shader for why). The kernel compacts
// survivors — plus the newborns it consumes from its tail range — into the other half
// of the container's ping-pong buffer pair, which drawParticles reads after the
// compute/graphics barrier in encode.
//
// alive is advanced by the newborn count uploaded this call, never read back down as
// particles die on the GPU: a deliberate, documented CPU-side over-estimate (see
// particleData.alive) that trades some capacity headroom for needing no GPU readback.
func (r *Renderer) dispatchParticleUpdate(cmd gpu.CommandBuffer, st *renderState, p *scenes.FramePacket) {
	for _, pp := range p.Particles.Data {
		births := pp.Newborns.Count
		if pp.Capacity == 0 && births == 0 {
			continue
		}
		ps := st.particleFor(pp.ID)
		ps.seen = true
		r.ensureParticleBuffers(ps, pp, births)

		if births > 0 {
			ps.pendingBuf.Write(utils.ToBytesSlice(p.Newborns.Data[pp.Newborns.First:][:births]), 0)
		}
		data := utils.ToBytes(&indirectCmd{
			indexCount: r.GeometryStore.IndexCount(pp.Geometry.Slot),
			firstIndex: r.GeometryStore.IndexBase(pp.Geometry.Slot),
		})
		ps.indirectBuf.Write(data, 0)

		var pendingAddr uint64
		if births > 0 {
			pendingAddr = ps.pendingBuf.Addr
		}
		ur := particleUpdateRoot{
			src: ps.buffers[ps.current].Addr, dst: ps.buffers[1-ps.current].Addr,
			pending: pendingAddr, indirect: ps.indirectBuf.Addr,
			capacity: pp.Capacity, pendingCount: births, dt: pp.DT,
			gravity: pp.Update.Gravity, drag: pp.Update.Drag,
		}
		if pp.Update.SizeOverLife.Enabled {
			ur.sizeEnabled, ur.sizeStart, ur.sizeEnd = 1, pp.Update.SizeOverLife.Start, pp.Update.SizeOverLife.End
		}
		if pp.Update.OpacityOverLife.Enabled {
			ur.opacityEnabled, ur.opacityStart, ur.opacityEnd = 1, pp.Update.OpacityOverLife.Start, pp.Update.OpacityOverLife.End
		}
		threads := pp.Capacity + births
		cmd.SetPipeline(r.particleUpdatePipeline)
		cmd.Dispatch(utils.ToBytes(&ur), (threads+63)/64, 1, 1)

		ps.current = 1 - ps.current
	}
	st.retireUnseenParticles(r.backend)
}

// ensureParticleBuffers allocates a system's ping-pong particle buffers and
// indirect-draw args on first use (sized to capacity, fixed at construction), and grows
// the newborn scratch buffer to fit this frame's births. A fresh or cleared system's
// particle buffers are zeroed so every slot starts dead (age 0 >= lifetime 0) rather
// than reading whatever was in freshly allocated memory — see particle_update.comp.glsl.
func (r *Renderer) ensureParticleBuffers(ps *particleState, pp scenes.ParticlePacket, births uint32) {
	if !ps.ready || ps.epoch != pp.Epoch {
		size := max(uint64(pp.Capacity)*uint64(particleRecordSize), 1)
		for i := range ps.buffers {
			if !ps.buffers[i].IsValid() || ps.buffers[i].Size < size {
				if ps.buffers[i].IsValid() {
					r.backend.Free(ps.buffers[i])
				}
				ps.buffers[i] = r.backend.Alloc(size, gpu.MemoryHost, "particles")
			}
			clear(unsafe.Slice((*byte)(ps.buffers[i].Ptr), ps.buffers[i].Size))
		}
		if !ps.indirectBuf.IsValid() {
			ps.indirectBuf = r.backend.Alloc(uint64(indirectSize), gpu.MemoryHost, "particles-indirect")
		}
		ps.current, ps.ready, ps.epoch = 0, true, pp.Epoch
	}
	if births > 0 {
		if size := uint64(births) * uint64(particleRecordSize); !ps.pendingBuf.IsValid() || ps.pendingBuf.Size < size {
			if ps.pendingBuf.IsValid() {
				r.backend.Free(ps.pendingBuf)
			}
			ps.pendingBuf = r.backend.Alloc(size, gpu.MemoryHost, "particles-pending")
		}
	}
}

// drawParticles issues one indirect draw per attached particle container, reading the
// instance count dispatchParticleUpdate's kernel just compacted. Each container's
// pipeline is resolved via the regular pipelineForMaterial path (its Material is a
// *materials.BasicParticleMaterial, whose Vertex/Forward already are the particle
// shaders — see basic_particle.go) and draws directly — unlike meshes, containers
// are never batched into drawList's multi-draw-indirect runs (one geometry/material
// per container already, so there is nothing to batch).
func (r *Renderer) drawParticles(cmd gpu.CommandBuffer, st *renderState, p *scenes.FramePacket, viewProj glm.Mat4f, eye glm.Vec3f) {
	idx := r.GeometryStore.IndexBuffer()
	for _, pp := range p.Particles.Data {
		ps, ok := st.particles[pp.ID]
		if !ok || !ps.ready {
			continue
		}
		id := p.Materials.Data[pp.Material]
		pool := r.MaterialStore.PoolAt(id.Pool)
		dr := particleDrawRoot{
			viewProj: viewProj,
			pos:      r.GeometryStore.PositionsAddr(), attr: r.GeometryStore.AttributesAddr(), descs: r.GeometryStore.DescriptorsAddr(),
			models: st.dl.worldBuf.Addr, particles: ps.buffers[ps.current].Addr,
			materials: pool.RecordsAddr(), lights: st.lights.Addr(),
			eye:        glm.Vec4f{eye[0], eye[1], eye[2], 1},
			geometryID: pp.Geometry.Slot, materialID: id.Slot, transformID: pp.Transform,
			time: p.Time,
		}
		cmd.SetPipeline(r.drawPipelines[r.pipelineForPool(pool, pool.Cull(id.Slot), pool.Blend(id.Slot))])
		cmd.DrawIndexedIndirect(utils.ToBytes(&dr), idx, gpu.IndexUint32, ps.indirectBuf, 0, 1, indirectSize)
	}
}

// recordShadowDepth renders the caster geometry into a light's depth map from its
// camera. It uses the view's own compacted-visible buffer (culled with castersOnly)
// and a single position-only MDI over every batch — material/pipeline don't matter
// for depth, so all batches draw with the one shadow pipeline.
func (r *Renderer) recordShadowDepth(cmd gpu.CommandBuffer, dl *drawList, v *drawView, sv shadowView) {
	width, height := sv.width, sv.height
	sr := shadowRoot{
		viewProj:  sv.cam.ViewProjection(),
		pos:       r.GeometryStore.PositionsAddr(),
		descs:     r.GeometryStore.DescriptorsAddr(),
		models:    dl.worldBuf.Addr,
		drawables: dl.drawableBuf.Addr,
		visible:   v.visibleBuf.Addr,
	}
	// Cascades share a texture, so only the first view clears it; the rest load what the
	// earlier ones drew and confine themselves to their own square with the scissor.
	load := gpu.LoadKeep
	if sv.clear {
		load = gpu.LoadClear
	}
	cmd.BeginRenderPass(gpu.RenderTargets{
		Depth: &gpu.DepthAttachment{Texture: r.TextureStore.GPU(sv.m), Load: load, Store: gpu.StoreKeep, Clear: 0.0},
	})
	cmd.SetViewport(float32(sv.x), 0, float32(width), float32(height), 0, 1)
	cmd.SetScissor(sv.x, 0, int32(width), int32(height))

	cmd.SetPipeline(r.shadowPipeline)
	cmd.DrawIndexedIndirect(utils.ToBytes(&sr), r.GeometryStore.IndexBuffer(), gpu.IndexUint32, v.indirectBuf, 0, uint32(dl.batchCount()), indirectSize)
	cmd.EndRenderPass()
}

// fillDrawRoots writes each pipeline run's drawRoot (the same layout serves both the
// forward and the G-buffer pass — a gbuffer fragment shader just doesn't read
// lights/eye/shadowSampler). Filling every run regardless of pass keeps this call
// independent of which passes actually run this frame; issueDraws filters by pass.
func (r *Renderer) fillDrawRoots(dl *drawList, viewProj glm.Mat4f, eye glm.Vec3f, lightsAddr uint64, elapsed float32) {
	if len(dl.runs) == 0 {
		return
	}
	// Roots now travel inline with each draw, so they live in a plain slice the draw
	// list reuses rather than in a GPU buffer.
	if cap(dl.roots) < len(dl.runs) {
		dl.roots = make([]drawRoot, len(dl.runs))
	}
	roots := dl.roots[:len(dl.runs)]
	for ri := range dl.runs {
		run := &dl.runs[ri]
		roots[ri] = drawRoot{
			viewProj:      viewProj,
			pos:           r.GeometryStore.PositionsAddr(),
			attr:          r.GeometryStore.AttributesAddr(),
			descs:         r.GeometryStore.DescriptorsAddr(),
			models:        dl.worldBuf.Addr,
			drawables:     dl.drawableBuf.Addr,
			visible:       dl.visibleBuf.Addr,
			materials:     run.pool.RecordsAddr(),
			lights:        lightsAddr,
			eye:           glm.Vec4f{eye[0], eye[1], eye[2], 1},
			shadowSampler: r.shadowSampler.Index,
			time:          elapsed,
		}
	}
}

// issueDraws records one multi-draw-indirect call per pipeline run belonging to
// pass: all of a material type's per-geometry commands go in a single
// DrawIndexedIndirect. Each command's
// firstInstance is its region base, so gl_InstanceIndex indexes the compacted visible
// buffer directly — no per-command push constant.
func (r *Renderer) issueDraws(cmd gpu.CommandBuffer, dl *drawList, p pass) {
	if len(dl.runs) == 0 {
		return
	}
	idx := r.GeometryStore.IndexBuffer()
	for ri := range dl.runs {
		run := &dl.runs[ri]
		key := r.drawPipelineKeys[run.pipeline]
		if key.pass != p {
			continue
		}
		cmd.SetPipeline(r.drawPipelines[run.pipeline])
		cmd.DrawIndexedIndirect(utils.ToBytes(&dl.roots[ri]), idx, gpu.IndexUint32, dl.indirectBuf, uint64(run.firstBatch)*uint64(indirectSize), run.count, indirectSize)
	}
}

// ensureGBufferSampler creates the sampler both fullscreen passes read the G-buffer
// with, on first use.
//
// Plain (non-comparison), unlike shadowSampler, which is compare-mode-only and would
// return comparison results instead of raw values if reused here.
//
// Point filtering, deliberately: these passes read texel-for-texel, and the data is
// not filterable — interpolating an octahedral normal, a packed model id or a depth
// value between texels is meaningless. Linear filtering on a D32_SFLOAT image is also
// not guaranteed to be supported.
func (r *Renderer) ensureGBufferSampler() {
	if r.gbufferSampler.H != 0 {
		return
	}
	r.gbufferSampler = r.backend.CreateSampler(gpu.SamplerDescriptor{
		AddressU: gpu.AddressClamp, AddressV: gpu.AddressClamp,
		Label: "gbuffer-read",
	})
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
	r.ensureGBufferSampler()
	invViewProj := viewProj.Inv()
	lr := lightingRoot{
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
			Color: []gpu.ColorAttachment{{Texture: target, Load: gpu.LoadClear, Store: gpu.StoreKeep, Clear: r.clear}},
			Depth: &gpu.DepthAttachment{Texture: r.depth, Load: gpu.LoadKeep, Store: gpu.StoreKeep, ReadOnly: true},
		})
		cmd.SetViewport(0, 0, float32(r.width), float32(r.height), 0, 1)
		cmd.SetScissor(0, 0, int32(r.width), int32(r.height))
		cmd.SetPipeline(r.lightingPipelines[k.lightingIdx])
		cmd.Draw(utils.ToBytes(&lr), 3, 1, 0, 0)
		cmd.EndRenderPass()
	}
}

// buildOverlay composes this frame's overlay from everything that draws into it: the
// FPS HUD and the console, in that order (the console panel covers the HUD when open).
func (r *Renderer) buildOverlay() {
	r.ensureOverlay()
	if r.overlay == nil {
		return
	}
	r.overlay.reset()
	if r.showFPS {
		// Logical points, scaled and snapped by the overlay (which rounds down to a
		// whole multiple of the font's cell, so this is an upper bound, not exact).
		const pt, gap, inset = 12, 15, 8
		size := r.scale * pt
		ms := func(d time.Duration) float64 { return float64(d.Microseconds()) / 1000 }
		x, y := r.scale*inset, r.scale*inset
		step := r.scale * gap
		r.overlay.text(fmt.Sprintf("FPS %.0f", r.stats.FPS()), x, y, size, r.fontColor)
		r.overlay.text(fmt.Sprintf("CPU %.2f ms", ms(r.stats.AvgCPUTime())), x, y+step, size, r.fontColor)
		if r.gpuValid {
			r.overlay.text(fmt.Sprintf("GPU %.2f ms", ms(r.stats.AvgGPUTime())), x, y+2*step, size, r.fontColor)
		}
	}
	if r.console != nil {
		r.console.Draw(consolePainter{r.overlay}, float32(r.width), float32(r.height))
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
	for id := range r.sources {
		r.ReleaseSource(id)
	}
	if r.overlay != nil {
		r.overlay.destroy()
	}
	if r.gpuPool.IsValid() {
		r.backend.DestroyTimestampPool(r.gpuPool)
	}
	if r.pipelinesReady {
		r.backend.DestroyPipeline(r.cullPipeline)
		r.backend.DestroyPipeline(r.skinPipeline)
		r.backend.DestroyPipeline(r.particleUpdatePipeline)
		r.backend.DestroyPipeline(r.shadowPipeline)
		for _, p := range r.drawPipelines {
			r.backend.DestroyPipeline(p)
		}
		for _, p := range r.lightingPipelines {
			r.backend.DestroyPipeline(p)
		}
	}
	if r.depth.IsValid() {
		r.backend.DestroyTexture(r.depth)
	}
	r.destroyGBuffer()
	if r.gbufferSampler.H != 0 {
		r.backend.DestroySampler(r.gbufferSampler)
	}
	if r.shadowSampler.H != 0 {
		r.backend.DestroySampler(r.shadowSampler)
	}
	if r.ownsTarget && r.target.IsValid() {
		r.backend.DestroyTexture(r.target)
	}
	if r.readback.IsValid() {
		r.backend.Free(r.readback)
	}
	r.GeometryStore.Destroy()
	r.MaterialStore.Destroy()
	r.TextureStore.Destroy()
	r.uploader.Destroy()
	r.backend.Destroy()
}

//TODO: Add a resize method that would adjust the SwapChain size
