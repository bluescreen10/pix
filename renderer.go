package pix

import (
	"fmt"
	"image"
	"image/png"
	"math"
	"math/bits"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"time"
	"unsafe"

	"github.com/bluescreen10/pix/postprocess"

	"github.com/bluescreen10/gamekit"
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
// renders a Scene from the cameras in it. It renders either to a window swapchain (see the
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
	skinPipeline           gpu.Pipeline // compute pre-skinning (scene_skin.comp) — see encodeSkinning
	particleUpdatePipeline gpu.Pipeline // compute particle simulation (particle_update.comp) — see encodeParticleSimulation
	// particleSortKeysPipeline and particleSortStepPipeline sort a container's particles
	// back to front (particle_sort_keys.comp, particle_sort_step.comp) — see
	// encodeParticleSorting.
	particleSortKeysPipeline gpu.Pipeline
	particleSortStepPipeline gpu.Pipeline
	// skinScratch and shadowViews collect the frame's skinning jobs and shadow views
	// (see skinCommands, collectShadowViews); reused every frame rather than
	// reallocated.
	skinScratch []skinCmd
	shadowViews []view
	// Draw pipelines, one per distinct material pipeline key (shaders + cull + blend).
	// drawPipelineKeys is parallel so pipelineFor can dedup and buildPipelines can
	// rebuild them all when the target format changes.
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

	// debugView draws the scene with a dedicated fragment shader instead of shading it;
	// one pipeline per view, built on first use since most frames never need any of
	// them (see debug_view.go).
	debugView      DebugView
	debugPipelines [debugViewCount]gpu.Pipeline

	// HDR. With it on, the scene is shaded into sceneColor, linear and unclamped; the
	// post-processing chain ping-pongs between it and postColor; the tone-map pass maps
	// the result into the target. With it off none of these exist, and the scene draws
	// straight into the target. Each step owns whatever else it draws with.
	hdr            bool
	toneMapping    ToneMapOperator
	exposureStops  float32
	postProcessing []postprocess.Step
	sceneColor     gpu.Texture
	postColor      gpu.Texture // only while the chain has steps
	linearSampler  gpu.Sampler
	toneMapPass    *postprocess.FullscreenPass

	// Volumetric fog: how finely it is simulated; the medium volume the inject pass writes
	// and the fog volume the integrate pass accumulates from it, created when a scene
	// first asks for volumetric fog; the two passes' pipelines; and the pass that fogs
	// the background, built for the format it was last drawn into.
	volumetricFog        VolumetricFogSettings
	fogMedium, fogVolume gpu.Texture
	fogInjectPipeline    gpu.Pipeline
	fogIntegratePipeline gpu.Pipeline
	fogBackgroundPass    *postprocess.FullscreenPass
	fogBackgroundFormat  gpu.Format

	// frameSteps is the work added at each stage of the frame, in the order it runs; and
	// stepFrame what the steps are told, rebuilt every frame (see frame_step.go).
	frameSteps [frameStageCount][]FrameStep
	stepFrame  Frame

	// Shadows: global toggle + the shared PCF comparison sampler (created lazily) +
	// the position-only depth-pass pipeline (rebuilt with the others on format change).
	// shadowDistance caps how far down the view frustum directional shadows are fit
	// (0 = auto: reach the far side of the scene sphere).
	// shadowAlgorithm picks how directional shadow cameras are fitted (see
	// ShadowAlgorithm and shadow_fit.go). Spot and point lights ignore it.
	shadowsEnabled bool
	shadowSampler  gpu.Sampler
	shadowPipeline gpu.Pipeline
	// prepassPipelines fill depth for the forward pass so shading runs once per pixel
	// (see Renderer.EnableDepthPrepass), one per materials.CullMode.
	prepassPipelines [3]gpu.Pipeline
	depthPrepass     bool
	shadowDistance   float32
	shadowNear       float32
	shadowFilter     ShadowFilter
	// shadows is how directional lights are fitted plus that fit's own settings; nil
	// means ShadowUniform (see Renderer.Shadows).
	shadows ShadowSettings

	// pendingShot is a queued frame capture, recorded into the frame being built (see
	// screenshot.go).
	pendingShot *screenshot

	// Console: nil until EnableConsole. It shares the debug overlay with the FPS HUD,
	// so either one being active is what keeps the overlay alive.
	console *console.Console

	// Stats HUD, and the profiler it reads (see profiler.go). GPU timing is armed only
	// while the HUD is showing.
	profiler  Profiler
	fontColor colors.RGBA32F
	showFPS   bool
	overlay   *overlay
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
		clear:     colors.RGBA32F{0, 0, 0, 1},       //TODO: extract as constant
		fontColor: colors.RGBA32F{1, 0.787, 0.1, 1}, //TODO: extract as constant
		// One uploader for the process, not one per frame: its staging arena only
		// pays off by keeping its memory across frames (see uploader).
		uploader:      newUploader(backend),
		GeometryStore: geometries.NewStore(backend),
		TextureStore:  textures.NewStore(backend),
		MaterialStore: materials.NewStore(backend),
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

// NewOffscreenRenderer is a convenience for a headless renderer with an internally-
// owned RGBA8 target of the given size (read it with Pixels/Capture). Equivalent to
// NewRenderer(&RendererConfig{Width, Height}); kept as an ergonomic shorthand.
func NewOffscreenRenderer(w, h uint32) (*Renderer, error) {
	return NewRenderer(&RendererConfig{Width: w, Height: h})
}

// attachWindow lets the windowing package own platform handles and surface
// creation. Pix only needs the resulting opaque surface and swapchain extent.
func (r *Renderer) attachWindow(w *gamekit.Window, width, height uint32) error {
	surface, err := w.CreateSurface(r.backend)
	if err != nil {
		return err
	}
	format, err := sRGBSwapchainFormat(r.backend.SwapchainFormats(surface))
	if err != nil {
		return err
	}
	r.swapchain, err = r.backend.CreateSwapchain(surface, gpu.SwapchainDescriptor{Width: width, Height: height, Format: format})
	if err != nil {
		return err
	}
	sizer, ok := r.backend.(swapchainSizer)
	if !ok {
		return fmt.Errorf("backend does not expose swapchain size")
	}
	sw, sh := sizer.SwapchainSize(r.swapchain)
	r.hasTarget = false
	r.clear = colors.RGBA32F{}
	r.configure(sw, sh, r.backend.SwapchainFormat(r.swapchain))
	return nil
}

// sRGBSwapchainFormat picks the backbuffer format for a window: an 8-bit sRGB one, in
// whichever channel order the surface offers. Every shader writes linear light, and the
// hardware encodes it for display as it stores — and decodes it to blend, so blending
// is linear too.
func sRGBSwapchainFormat(available []gpu.Format) (gpu.Format, error) {
	for _, format := range []gpu.Format{gpu.FormatBGRA8Srgb, gpu.FormatRGBA8Srgb} {
		if slices.Contains(available, format) {
			return format, nil
		}
	}
	return gpu.FormatUndefined, fmt.Errorf("the window presents no 8-bit sRGB format (it offers %v)", available)
}

// attachTexture configures an internally-owned sRGB RGBA8 render target of w×h.
func (r *Renderer) attachTexture(w, h uint32) {
	tex := r.backend.CreateTexture(gpu.TextureDescriptor{Kind: gpu.Texture2D, Width: w, Height: h,
		Format: gpu.FormatRGBA8Srgb, Usage: gpu.TextureRenderTarget | gpu.TextureTransfer})
	r.ownsTarget = true
	r.SetRenderTarget(tex, w, h, gpu.FormatRGBA8Srgb)
}

// SetRenderTarget renders into tex (headless). The renderer (re)creates its depth
// buffer and pipelines to match. tex must have render-target usage (plus transfer
// usage if you intend to Capture it).
//
// The format should be an sRGB one (FormatRGBA8Srgb, FormatBGRA8Srgb): every shader
// writes linear light and relies on the target to encode it for display. A unorm target
// stores the linear values as they are, and the image comes out too dark.
func (r *Renderer) SetRenderTarget(tex gpu.Texture, w, h uint32, format gpu.Format) {
	r.target = tex
	r.hasTarget = true
	// The swapchain handle is left intact: rendering to a target can be temporary, and
	// hasTarget (not a nil swapchain) is what selects the destination — so a later
	// hasTarget=false renders to the window again without recreating the swapchain.
	r.configure(w, h, format)
}

// configure sets the size/format and (re)creates the depth buffer and the pipelines.
func (r *Renderer) configure(w, h uint32, format gpu.Format) {
	r.width, r.height, r.color = w, h, format
	if r.depth.IsValid() {
		r.backend.DestroyTexture(r.depth)
	}
	// Sampled too, so a pass can read depth back rather than only test against it.
	r.depth = r.backend.CreateTexture(gpu.TextureDescriptor{Kind: gpu.Texture2D, Width: w, Height: h,
		Format: gpu.FormatDepth32F, Usage: gpu.TextureDepth | gpu.TextureSampled, Label: "depth"})

	// With HDR on the scene renders into an image of the target's size, not the target
	// itself. The chain's second image is created when a step first needs it, and steps
	// with images of their own resize them on their next Encode.
	r.releaseHDRImages()
	if r.hdr {
		r.sceneColor = postprocess.CreateImage(r.backend, w, h, "scene-color")
	}

	r.buildPipelines()
}

// sceneFormat is what the scene is shaded into: the HDR scene image, or the target.
func (r *Renderer) sceneFormat() gpu.Format {
	if r.hdr {
		return postprocess.ImageFormat
	}
	return r.color
}

// releaseHDRImages frees the scene image and the chain's second image, if they exist.
func (r *Renderer) releaseHDRImages() {
	for _, image := range []*gpu.Texture{&r.sceneColor, &r.postColor} {
		if image.IsValid() {
			r.backend.DestroyTexture(*image)
		}
		*image = gpu.Texture{}
	}
}

// --------------------------------------------------------------------------------------
// Settings and inspection
//
// The public knobs: what the renderer draws into, how it looks, what it shows, and the
// read-only views into what it has computed.
// --------------------------------------------------------------------------------------

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

// ClearColor is the colour the frame is cleared to (see SetClearColor).
func (r *Renderer) ClearColor() colors.RGBA32F {
	return r.clear
}

// SetClearColor sets the colour the frame is cleared to, in linear light like every
// other colour the renderer is given.
func (r *Renderer) SetClearColor(rgba colors.RGBA32F) {
	r.clear = rgba
}

// FontColor is the colour the debug HUD's text is drawn in.
func (r *Renderer) FontColor() colors.RGBA32F {
	return r.fontColor
}

// SetFontColor sets the colour of the debug HUD's text.
func (r *Renderer) SetFontColor(rgba colors.RGBA32F) {
	r.fontColor = rgba
}

// ShowFPS toggles the debug HUD (FPS + CPU + GPU frame times, in the bitmap font).
func (r *Renderer) ShowFPS(on bool) {
	r.showFPS = on
	if !on {
		r.profiler.disableGPUProfiling(r.backend)
		return
	}
	r.ensureOverlay()
	r.profiler.enableGPUProfiling(r.backend)
}

// StatsVisible reports whether the debug HUD is showing (see ShowFPS).
func (r *Renderer) StatsVisible() bool {
	return r.showFPS
}

// Profiler reports where recent frames spent their time — the same numbers ShowFPS
// draws onscreen, for callers that want them programmatically (an automated
// before/after comparison, say) rather than off the HUD. GPU figures are only recorded
// while ShowFPS(true) is active.
func (r *Renderer) Profiler() *Profiler {
	return &r.profiler
}

// EnableShadows globally enables or disables shadow rendering. When off, no shadow
// views are culled and no shadow passes run.
func (r *Renderer) EnableShadows(on bool) {
	r.shadowsEnabled = on
}

// ShadowsEnabled and ShadowDistance report the current setting of
// the matching Enable*/Set* call. They exist so these toggles can be bound to
// something that has to read them back — a console variable, a settings panel —
// without the caller keeping its own shadow copy in sync.
func (r *Renderer) ShadowsEnabled() bool {
	return r.shadowsEnabled
}

func (r *Renderer) ShadowDistance() float32 {
	return r.shadowDistance
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

// ShadowNear reports the distance ShadowCascaded starts its split from, or 0 when it is
// derived from the view.
func (r *Renderer) ShadowNear() float32 {
	return r.shadowNear
}

// SetShadowNear sets the distance, in world units from the eye, that ShadowCascaded
// starts its cascade split from. Zero (the default) derives it from the covered range.
//
// Geometry nearer than this is still shadowed — the first cascade is fitted from the
// camera's real near plane, and only the BOUNDARIES are computed from this. What it
// controls is the ratio between consecutive cascades, and that ratio is the whole story
// for quality: each boundary drops texel density by exactly that factor, so a split
// starting far too close forces large ratios and a visible cliff at every boundary. Set
// it to roughly where the nearest geometry the camera can see begins.
func (r *Renderer) SetShadowNear(distance float32) {
	r.shadowNear = distance
}

// Shadows reports how directional shadow cameras are fitted, and that fit's settings.
func (r *Renderer) Shadows() ShadowSettings {
	if r.shadows == nil {
		return ShadowUniform{}
	}
	return r.shadows
}

// SetShadows selects how directional shadow cameras are fitted — see ShadowSettings.
// It takes effect on the next Render: the fit is recomputed every frame, and a shadow
// map is reallocated only if the number of slices changed.
//
// This is not Renderer.EnableShadows, which turns shadow rendering on and off; this
// chooses how it is done when it is on.
func (r *Renderer) SetShadows(s ShadowSettings) {
	if s == nil {
		s = ShadowUniform{}
	}
	r.shadows = s
}

// ShadowFilter reports which kernel directional shadow lookups use.
func (r *Renderer) ShadowFilter() ShadowFilter {
	return r.shadowFilter
}

// SetShadowFilter selects the kernel directional shadow lookups use — see ShadowFilter.
// It takes effect on the next Render; nothing needs reallocating.
func (r *Renderer) SetShadowFilter(filter ShadowFilter) {
	r.shadowFilter = filter
}

// VolumetricFog returns how finely the renderer simulates volumetric fog, as last set
// (see SetVolumetricFog).
func (r *Renderer) VolumetricFog() VolumetricFogSettings {
	return r.volumetricFog
}

// SetVolumetricFog sets how finely the renderer simulates volumetric fog: the
// resolution of the volume of froxels laid over the camera's view, for any scene whose
// fog is a scenes.VolumetricFog. Finer costs more, in the two compute passes that fill
// the volume every frame and in the memory it takes. The volume is rebuilt at the new
// size the next time such a scene renders.
func (r *Renderer) SetVolumetricFog(settings VolumetricFogSettings) {
	r.volumetricFog = settings
	r.releaseFogVolumes()
}

// ShadowView returns the shadow resources the renderer holds for one light of one
// source, or nil if it has none — the light does not cast, shadows are disabled, or
// nothing has been rendered yet.
//
// This is the resource half of what LightShadow used to be. The settings half stayed on
// the light (see LightShadow); what could not stay is anything whose value depends on
// the view being rendered, which is all of this.
func (r *Renderer) ShadowView(source scenes.SourceID, light scenes.LightID) *ShadowView {
	st, ok := r.sources[source]
	if !ok {
		return nil
	}
	s, ok := st.shadows[light]
	if !ok {
		return nil
	}
	v := &ShadowView{Camera: s.cam, Map: s.m, Faces: s.faces}
	for _, c := range s.cascades {
		v.Cascades = append(v.Cascades, c.cam)
		v.Splits = append(v.Splits, c.far)
	}
	switch {
	case len(s.faces) > 0:
		v.Camera, v.Map = s.faces[0].cam, s.faces[0].m
	case len(v.Cascades) > 0:
		v.Camera = v.Cascades[0]
	}
	return v
}

// DepthPrepassEnabled reports whether the forward pass is preceded by a depth-only pass.
func (r *Renderer) DepthPrepassEnabled() bool {
	return r.depthPrepass
}

// EnableDepthPrepass turns on a depth-only pass over opaque geometry before the forward
// pass, so shading runs once per visible pixel instead of once per fragment drawn.
//
// Worth it on GPUs that shade in submission order — desktop NVIDIA and AMD — when the
// frame is fragment-bound and overdrawn. On Apple GPUs it is a pure cost: their
// hidden-surface removal already shades each opaque pixel once, so the prepass saves the
// forward pass nothing and adds a second submission of the geometry. Measured on the
// beach scene at 2560x1440 (median of 12 runs each): the forward pass took the same time
// either way, and the frame 0.2-1.0 ms longer with the prepass.
func (r *Renderer) EnableDepthPrepass(on bool) {
	r.depthPrepass = on
}

// DebugView reports which view is being displayed.
func (r *Renderer) DebugView() DebugView {
	return r.debugView
}

// SetDebugView draws the scene with one debug fragment shader instead of its
// materials'. DebugOff restores normal shading. Takes effect on the next Render.
func (r *Renderer) SetDebugView(v DebugView) {
	r.debugView = v
}

// HDREnabled reports whether frames are rendered in high dynamic range (see EnableHDR).
func (r *Renderer) HDREnabled() bool {
	return r.hdr
}

// EnableHDR renders frames in high dynamic range: the scene is shaded into an offscreen
// image in linear, unclamped light, the post-processing chain runs over it, and a
// tone-map pass maps the result into the target (see SetToneMapping and SetExposure).
//
// Off — the default — the scene draws straight into the target: no offscreen image, no
// extra pass, and no post-processing. Light brighter than white clips.
func (r *Renderer) EnableHDR(on bool) {
	if on == r.hdr {
		return
	}
	r.hdr = on
	// Without a target yet, configuring one later builds for the new setting.
	if r.width == 0 {
		return
	}
	r.configure(r.width, r.height, r.color)
}

// ToneMapping is the curve an HDR frame is mapped through (see SetToneMapping).
func (r *Renderer) ToneMapping() ToneMapOperator {
	return r.toneMapping
}

// SetToneMapping sets the curve an HDR frame's light is mapped through into the range a
// display can show. The default is ToneMapNeutral. It has no effect without HDR.
func (r *Renderer) SetToneMapping(operator ToneMapOperator) {
	r.toneMapping = operator
}

// Exposure is the scale applied to an HDR frame's light before tone mapping, in stops.
func (r *Renderer) Exposure() float32 {
	return r.exposureStops
}

// SetExposure scales an HDR frame's light before tone mapping, in stops: +1 doubles it,
// -1 halves it, and 0 — the default — leaves it unchanged. It has no effect without HDR.
func (r *Renderer) SetExposure(stops float32) {
	r.exposureStops = stops
}

// PostProcessing returns the effects the renderer runs over the shaded scene, in order.
func (r *Renderer) PostProcessing() []postprocess.Step {
	return r.postProcessing
}

// SetPostProcessing replaces the chain of effects run over the shaded scene, in the
// order given. Each step reads the image the one before it produced; the last one's
// result is tone-mapped into the target.
//
// The chain runs only while HDR is on (see EnableHDR): its effects work on the
// unclamped light that tone mapping then compresses. The default is an empty chain.
//
// Steps are held by pointer, so changing one's fields changes the next frame. A step
// that leaves the chain is released; it can be added back later.
func (r *Renderer) SetPostProcessing(steps []postprocess.Step) {
	for _, step := range r.postProcessing {
		if !slices.Contains(steps, step) {
			step.Release()
		}
	}
	r.postProcessing = slices.Clone(steps)
	// An empty chain needs no second image.
	if len(steps) == 0 && r.postColor.IsValid() {
		r.backend.DestroyTexture(r.postColor)
		r.postColor = gpu.Texture{}
	}
}

// AddPostProcessingStep appends one effect to the end of the chain.
//
// A step can be in the chain once: its images are sized for one place in it, and
// RemovePostProcessingStep would not know which to remove. Adding it again panics.
func (r *Renderer) AddPostProcessingStep(step postprocess.Step) {
	if slices.Contains(r.postProcessing, step) {
		panic("pix: AddPostProcessingStep: step is already in the chain")
	}
	r.SetPostProcessing(append(slices.Clone(r.postProcessing), step))
}

// RemovePostProcessingStep takes one effect out of the chain and releases it. Removing
// a step that is not in the chain does nothing.
func (r *Renderer) RemovePostProcessingStep(step postprocess.Step) {
	i := slices.Index(r.postProcessing, step)
	if i < 0 {
		return
	}
	r.SetPostProcessing(slices.Delete(slices.Clone(r.postProcessing), i, i+1))
}

// AddFrameStep adds step to the frame at stage, after the steps already there. Steps
// are held by pointer, so changing one's fields changes the next frame.
//
// A step can be added once: its resources are sized for one place in the frame, and
// RemoveFrameStep would not know which to remove. Adding it again panics.
func (r *Renderer) AddFrameStep(stage FrameStage, step FrameStep) {
	if r.hasFrameStep(step) {
		panic("pix: AddFrameStep: step is already added")
	}
	r.frameSteps[stage] = append(r.frameSteps[stage], step)
}

// RemoveFrameStep takes step out of the frame and releases it. Removing a step that
// was never added, or is already removed, does nothing.
func (r *Renderer) RemoveFrameStep(step FrameStep) {
	for stage, steps := range r.frameSteps {
		if i := slices.Index(steps, step); i >= 0 {
			r.frameSteps[stage] = slices.Delete(steps, i, i+1)
			step.Release()
			return
		}
	}
}

// hasFrameStep reports whether step is added at any stage.
func (r *Renderer) hasFrameStep(step FrameStep) bool {
	for _, steps := range r.frameSteps {
		if slices.Contains(steps, step) {
			return true
		}
	}
	return false
}

// hasFrameStepsAt reports whether any step is added at stage.
func (r *Renderer) hasFrameStepsAt(stage FrameStage) bool {
	return len(r.frameSteps[stage]) > 0
}

// EnableConsole turns on the developer console, reading from in, and returns it so
// values can be registered:
//
//	c := r.EnableConsole(gamekitinput.New(win))
//	console.Bind(c, "shadow.distance", &shadowDistance, "directional shadow fit")
//
// The console starts hidden; the user opens it with its toggle key (` by default).
// The renderer's own switches (shadows, the depth prepass, stats, the clear and HUD colours,
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

// Screenshot queues a PNG of the next completed frame and calls done (which may be
// nil) once it has been written or has failed.
//
// An empty path writes a timestamped file in the working directory. The callback is
// invoked from Render, on the frame that produced the image, so a console command can
// report the result into its own scrollback.
//
// It relies on the frame's colour target being readable, which for a window means the
// swapchain was created with transfer-source usage. Where a driver does not allow that,
// the callback reports it rather than the renderer failing quietly.
func (r *Renderer) Screenshot(path string, done func(path string, err error)) {
	if path == "" {
		path = fmt.Sprintf("screenshot-%s.png", time.Now().Format("20060102-150405"))
	}
	r.pendingShot = &screenshot{path: path, done: done}
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
	cmd.Barrier(gpu.StageColorOutput, gpu.StageTransfer, 0)
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

// --------------------------------------------------------------------------------------
// Resources
//
// Conveniences that create materials and geometry in the renderer's stores.
// --------------------------------------------------------------------------------------

// Shortcuts for the built-in material types on this renderer's MaterialStore.
//
// Geometry and textures need no equivalent: their store is the natural receiver, so
// r.GeometryStore.Create(cfg) already reads well. A material type has its own
// constructor per type, which pushes the store down into an argument —
// materials.NewPBRMaterial(r.MaterialStore) — and these hand that boilerplate back.
//
// They are shortcuts, not the API: a custom material type outside this package calls
// materials.New*/Pool.Create against r.MaterialStore directly, exactly as these do.

// NewBasicMaterial creates an unlit material with an unbound color map.
func (r *Renderer) NewBasicMaterial() *materials.BasicMaterial {
	return materials.NewBasicMaterial(r.MaterialStore)
}

// NewBasicParticleMaterial creates an unlit particle material with an unbound color
// map — the only material type ParticleConfig.Material accepts (see NewParticleContainer).
func (r *Renderer) NewBasicParticleMaterial() *materials.BasicParticleMaterial {
	return materials.NewBasicParticleMaterial(r.MaterialStore)
}

// NewBlinnPhongMaterial creates a Blinn-Phong material with an unbound color map.
func (r *Renderer) NewBlinnPhongMaterial() *materials.BlinnPhongMaterial {
	return materials.NewBlinnPhongMaterial(r.MaterialStore)
}

// NewPBRMaterial creates a PBR material with four unbound maps: color, normal,
// metallic and roughness.
func (r *Renderer) NewPBRMaterial() *materials.PBRMaterial {
	return materials.NewPBRMaterial(r.MaterialStore)
}

// NewRawMaterial creates a self-describing material from your own shader and record
// size: the store allocates storage from dataSize, and you write the record bytes
// yourself (see materials.RawMaterial).
func (r *Renderer) NewRawMaterial(shader materials.Shader, dataSize, textureSlots int) *materials.RawMaterial {
	return materials.NewRawMaterial(r.MaterialStore, shader, dataSize, textureSlots)
}

// NewBoxGeometry uploads a BoxGeometry and returns its handle.
func (r *Renderer) NewBoxGeometry(width, height, depth float32) geometries.Geometry {
	return r.GeometryStore.Create(BoxGeometry(width, height, depth))
}

// NewPlaneGeometry uploads a PlaneGeometry and returns its handle.
func (r *Renderer) NewPlaneGeometry(width, depth float32, widthSegments, depthSegments int) geometries.Geometry {
	return r.GeometryStore.Create(PlaneGeometry(width, depth, widthSegments, depthSegments))
}

// NewSphereGeometry uploads a SphereGeometry and returns its handle.
func (r *Renderer) NewSphereGeometry(radius float32, widthSegments, heightSegments int) geometries.Geometry {
	return r.GeometryStore.Create(SphereGeometry(radius, widthSegments, heightSegments))
}

// NewCylinderGeometry uploads a CylinderGeometry and returns its handle.
func (r *Renderer) NewCylinderGeometry(radiusTop, radiusBottom, height float32, radialSegments, heightSegments int) geometries.Geometry {
	return r.GeometryStore.Create(CylinderGeometry(radiusTop, radiusBottom, height, radialSegments, heightSegments))
}

// NewCapsuleGeometry uploads a CapsuleGeometry and returns its handle.
func (r *Renderer) NewCapsuleGeometry(radius, length float32, capSegments, radialSegments int) geometries.Geometry {
	return r.GeometryStore.Create(CapsuleGeometry(radius, length, capSegments, radialSegments))
}

// NewDecalGeometry clips geometry against a projection box and returns the resulting
// patch. Position, orientation, and size use the source geometry's local space, and
// the returned geometry uses that same space.
//
//	geo := r.NewDecalGeometry(box.Geometry(), localHit, orientation, glm.Vec3f{0.5, 0.5, 0.5})
//	decal := scene.NewMesh(geo, decalMat)
//	decal.SetCastShadow(false)
//	box.Add(decal)
//
// It returns the zero Geometry when the box misses the source or every candidate
// triangle faces away. The result belongs to the source geometry's Store.
func (r *Renderer) NewDecalGeometry(geometry geometries.Geometry, position glm.Vec3f, orientation glm.Quatf, size glm.Vec3f) geometries.Geometry {
	return geometry.Clip(position, orientation, size)
}

// --------------------------------------------------------------------------------------
// The frame
//
// The forward path, from a scene to pixels. Render is the recipe, and every step it calls
// follows in the order it calls them, so this section reads top to bottom as a frame.
// --------------------------------------------------------------------------------------

// Render draws one frame of the scene into the renderer's target — a window's swapchain
// or an offscreen texture — seen from the first view the scene describes: for a Scene,
// its first attached, visible camera. Further views are not drawn yet.
//
// A scene with no view still produces a frame: the target is cleared, and the overlay
// and particle simulation run as usual.
func (r *Renderer) Render(scene scenes.Producer) {

	if !r.hasTarget && !r.swapchain.IsValid() {
		panic("renderer has no target")
	}

	// 1. Run the console before anything of the frame exists. Its commands change the
	// renderer's settings, and some — switching HDR, say — rebuild resources the frame
	// would be recording against, uploading some of them with a command buffer of their
	// own. They have to land between frames, not in the middle of one.
	r.updateConsole()

	// 2. Begin the frame. One command buffer holds all of it: the shared uploads record
	// into it ahead of the frame's work, ordered against it by a barrier, rather than
	// paying for a submit of their own.
	r.profiler.beginFrame()
	cmd := r.backend.Begin()

	// 3. Extract: compare the scene's packet against this source's GPU state, and
	// re-sync whatever has gone stale.
	st, views := r.extract(scene)

	// 4. Build the overlay — the HUD and the console — on the CPU.
	r.buildOverlay()

	// 5. Acquire the target, upload the resources every scene shares, and encode the
	// frame's GPU work.
	//
	// The upload comes after the acquire deliberately. A shared store may reallocate a
	// buffer while it syncs, and the acquire is what waits out the frame still using the
	// old one — uploading earlier could swap resources out from under a frame in flight.
	// It also has to precede everything that builds a root, since those read the
	// buffers' device addresses.
	target := r.acquireTarget()
	r.uploadSharedResources(cmd)
	r.encode(st, views, target, cmd)

	// 6. Capture, before the overlay goes on top: a screenshot is of the scene, not of
	// the tools used to inspect it.
	r.recordScreenshot(target, cmd)

	// 7. Draw the overlay.
	r.encodeOverlay(target, cmd)

	// 8. Submit, then finish the frame.
	r.submit(cmd)
	r.finishFrame()
}

// ---------------------------------------------------------------------------------------
// 3. Extract
// ---------------------------------------------------------------------------------------

// updateConsole lets the console read this frame's input and run what it was given. It
// reads input every frame, open or not: the key that opens it arrives while it is
// closed.
func (r *Renderer) updateConsole() {
	if r.console != nil {
		r.console.Update()
	}
}

// extract brings one source's GPU state up to date with the frame its scene describes,
// and returns that state along with the views the frame renders from.
//
// Everything a packet says becomes renderer-owned state here, and nowhere else. Once it
// returns, nothing downstream reads the scene.
func (r *Renderer) extract(scene scenes.Producer) (*renderState, frameViews) {
	// 3a. Look up the source's state, and take the scene's packet. Extraction settles
	// the scene first — transforms, skinning, the clock — and must happen exactly once
	// a frame: a second call would get a packet whose per-frame flags, such as
	// TransformsDirty, the first had already consumed.
	st := r.stateFor(scene.ID())
	scene.Extract(&r.frame)
	p := &r.frame

	// 3b–3f. Re-sync each part of the state that the packet describes.
	r.extractTransforms(p, st)
	views := r.extractViews(p, st)
	r.extractLights(p, st)
	r.collectDrawables(p, st)
	r.extractParticles(p, st)

	// Everything the packet describes is now renderer-owned — its particle births are
	// copied out of the scene, not borrowed — so the producer can retire this frame's
	// simulation step. One function owning both extract and retire is what makes each
	// step happen exactly once without an acknowledgement protocol.
	scene.Rendered()
	return st, views
}

// extractTransforms uploads the matrices the vertex stages read: every node's world
// matrix followed by every instanced mesh's per-instance transforms, as one array, and
// the joint palettes compute skinning reads.
//
// gpuDrawable.transformID indexes the combined world array — an instanced mesh's
// drawables address their slice as if it sits right after the node matrices, which is
// exactly where it does sit.
func (r *Renderer) extractTransforms(p *scenes.FramePacket, st *renderState) {
	// The producer says whether the matrices moved; a frame where nothing did writes
	// nothing. A state seeing its first packet uploads regardless: the dirty flag reports
	// change since the last EXTRACTION, which a state created just now never saw, and a
	// source released and drawn again would otherwise render from an unwritten buffer.
	if p.TransformsDirty || !st.worldBuf.IsValid() {
		nodes := p.Transforms.Data
		instances := p.InstanceTransforms.Data

		r.growBuffer(&st.worldBuf, uint64(len(nodes)+len(instances))*matrixSize, "world")
		if len(nodes) > 0 {
			st.worldBuf.Write(utils.ToBytesSlice(nodes), 0)
		}
		if len(instances) > 0 {
			st.worldBuf.Write(utils.ToBytesSlice(instances), uint64(len(nodes))*matrixSize)
		}
	}

	if joints := p.Joints.Data; len(joints) > 0 {
		r.growBuffer(&st.jointBuf, uint64(len(joints))*matrixSize, "joints")
		st.jointBuf.Write(utils.ToBytesSlice(joints), 0)
	}
}

// matrixSize is the size of one world or joint matrix in a GPU buffer.
const matrixSize = 64

// extractViews works out every camera the frame renders from: the main camera, and —
// when shadows are on — a camera for every shadow map region, fitted to this frame.
//
// The main camera is the packet's first view. The others are not rendered yet: each
// would need its own viewport, cull, and shadow fit.
func (r *Renderer) extractViews(p *scenes.FramePacket, st *renderState) frameViews {
	if len(p.Views) == 0 {
		return frameViews{}
	}

	mainView := p.Views[0]
	vp := mainView.ViewProjection()
	views := frameViews{
		main:        view{viewProj: flipClipY(vp), planes: glm.FrustumPlanes(vp), cull: &st.mainCull},
		hasMainView: true,
		eye:         mainView.Position,
	}
	if !r.shadowsEnabled {
		return views
	}

	r.fitShadows(mainView, p, st)
	views.shadows = r.collectShadowViews(p, st)
	return views
}

// fitShadows gives every casting light the depth maps and cameras it needs this frame,
// and retires the resources of lights that stopped casting.
//
// The packet says only which lights cast and at what resolution. Everything here —
// which projection, where it points, how it is fitted to the view, what the depth bias
// works out to — is the renderer's, because it all depends on the view being rendered
// and on caster bounds the producer does not track.
//
// Depth maps are ensured here rather than when they are drawn into, for two reasons: the
// fit derives its depth bias from how much world one texel covers, and the light table
// built next carries each map's bindless index.
func (r *Renderer) fitShadows(mainView scenes.ViewPacket, p *scenes.FramePacket, st *renderState) {
	r.ensureShadowSampler()

	// Point lights first: they are aimed from the light alone and need no scene bounds.
	var needsFit bool
	for _, light := range p.Lights.Data {
		if !light.CastsShadow {
			continue
		}
		if light.Kind != scenes.LightPoint {
			needsFit = true
			continue
		}
		r.aimPointShadow(r.shadowResourceFor(light.ID, st), light)
	}

	if needsFit {
		center, radius := casterBounds(p)
		fit := r.shadowFitFor(mainView, center, radius, r.shadowReach())
		for _, light := range p.Lights.Data {
			if !light.CastsShadow || light.Kind == scenes.LightPoint {
				continue
			}
			sh := r.shadowResourceFor(light.ID, st)
			switch light.Kind {
			case scenes.LightDirectional:
				// One square per cascade, laid out along the width: four 1024 cascades
				// are a single 4096x1024 texture, each rendered through its own scissor.
				width, height := cascadeAtlas(requestedSize(light), uint32(r.Shadows().levels()))
				sh.ensureMap(r.TextureStore, width, height)
				r.fitDirectional(sh, light, fit)
			case scenes.LightSpot:
				sh.ensurePerspective(light.Angle, light.Range)
				sh.ensureMap(r.TextureStore, requestedSize(light), requestedSize(light))
				aimSpotShadow(sh, light)
			}
		}
	}

	r.retireUnusedShadows(st)
}

// collectShadowViews turns the fitted shadow resources into views, in packet light order
// so a frame's shadow passes run in the same order every time.
//
// A directional or spot light contributes one view, a cascaded light one per cascade —
// all sharing its atlas, with only the first clearing it — and a point light one per
// cube face. After this, light types no longer matter: every view is culled for casters
// and drawn into its region the same way.
func (r *Renderer) collectShadowViews(p *scenes.FramePacket, st *renderState) []view {
	views := r.shadowViews[:0]
	for _, light := range p.Lights.Data {
		sh, ok := st.shadows[light.ID]
		if !light.CastsShadow || !ok {
			continue
		}
		switch {
		case len(sh.faces) > 0:
			for _, face := range sh.faces {
				if face.m.IsValid() {
					views = append(views, shadowView(face.cam, face.m, 0, sh.width, sh.height, true))
				}
			}
		case !sh.m.IsValid():
		case len(sh.cascades) > 0:
			side := sh.size()
			for i, cascade := range sh.cascades {
				views = append(views, shadowView(cascade.cam, sh.m, int32(i)*int32(side), side, side, i == 0))
			}
		default:
			views = append(views, shadowView(sh.cam, sh.m, 0, sh.width, sh.height, true))
		}
	}

	// Each view gets its own cull buffers. The pool is grown before any pointer into it
	// is taken, so no pointer is left behind by a reallocation.
	for len(st.shadowCulls) < len(views) {
		st.shadowCulls = append(st.shadowCulls, cullBuffers{})
	}
	for i := range views {
		views[i].cull = &st.shadowCulls[i]
	}

	r.shadowViews = views
	return views
}

// shadowView is a view rendering one region of a shadow map from cam.
func shadowView(cam Camera, shadowMap textures.Texture, x int32, width, height uint32, clearsMap bool) view {
	vp := cam.ViewProjection()
	return view{
		viewProj:    vp,
		planes:      glm.FrustumPlanes(vp),
		castersOnly: true,
		shadowMap:   shadowMap,
		x:           x,
		width:       width,
		height:      height,
		clearsMap:   clearsMap,
	}
}

// extractLights packs the packet's lights and the shadow resources fitted above into the
// GPU light table. The producer never sees a map index.
//
// The global shadow toggle goes in with them: without it, disabling shadows would only
// stop the maps being refreshed, and the shader would keep sampling the last ones.
func (r *Renderer) extractLights(p *scenes.FramePacket, st *renderState) {
	st.lights.rebuild(p.Environment, p.Lights.Data, st.shadows, r.shadowsEnabled, r.shadowFilter, r.fogLookupFor(p.Environment.Fog))
	st.lights.Sync()
}

// fogLookupFor returns where lit shaders find a volumetric fog's volume, or a zero
// lookup when the fog is not volumetric. The volume is encoded later in the frame, but
// it is ensured here, ahead of that: the light table, written now, carries its index.
func (r *Renderer) fogLookupFor(fog scenes.FogState) fogLookup {
	if fog.Mode != scenes.FogVolumetric {
		return fogLookup{}
	}
	size := r.ensureFogVolumes()
	return fogLookup{
		volume:  r.fogVolume.Index,
		sampler: r.ensureLinearSampler().Index,
		slices:  size.Depth,
		width:   r.width,
		height:  r.height,
	}
}

// collectDrawables rebuilds the draw layout and its buffers when the layout no longer
// describes the scene, and does nothing otherwise — rebuilding walks every drawable in
// the scene, so a steady frame must not pay for it.
func (r *Renderer) collectDrawables(p *scenes.FramePacket, st *renderState) {
	if !isLayoutStale(p, &st.layout) {
		return
	}

	buildDrawables(p, &st.layout, r.MaterialStore)
	orderBatches(&st.layout, r.GeometryStore)
	r.uploadLayout(st)
	st.layout.meshRevision = p.Meshes.Revision
}

// uploadLayout writes the layout's tables into the buffers the GPU reads them from. The
// cull buffers are sized later, per view, when the cull is encoded.
func (r *Renderer) uploadLayout(st *renderState) {
	layout := &st.layout
	r.growBuffer(&st.drawableBuf, uint64(max(len(layout.drawables), 1))*uint64(drawableSize), "drawables")
	r.growBuffer(&st.lodTableBuf, uint64(len(layout.lods))*uint64(lodEntrySize), "lod-table")

	if len(layout.drawables) > 0 {
		st.drawableBuf.Write(utils.ToBytesSlice(layout.drawables), 0)
	}
	st.lodTableBuf.Write(utils.ToBytesSlice(layout.lods), 0)

	// Reset every transform's LOD hysteresis. A rebuild may have added, removed or
	// reassigned lodID/lodLevel, so stale "level shown last frame" state could pick a
	// level that no longer matches this drawable set. Losing stickiness for one frame
	// right after a rebuild is an accepted tradeoff — rebuilds are already disruptive.
	transformSlots := max(st.worldBuf.Size/matrixSize, 1)
	r.growBuffer(&st.prevLevelBuf, transformSlots*4, "lod-prev-level")
	reset := make([]uint32, transformSlots)
	for i := range reset {
		reset[i] = lodNoneSentinel
	}
	st.prevLevelBuf.Write(utils.ToBytesSlice(reset), 0)
}

// extractParticles readies every particle system in the packet for this frame's step:
// its simulation buffers, this frame's births, and a fresh indirect command for the
// kernel to count survivors into. Systems that left the packet are retired, and their
// simulation with them — there is nothing on the CPU to restore it from.
func (r *Renderer) extractParticles(p *scenes.FramePacket, st *renderState) {
	for _, pp := range p.Particles.Data {
		births := pp.Newborns.Count
		if pp.Capacity == 0 && births == 0 {
			continue
		}

		ps := r.particleStateFor(pp.ID, st)
		r.ensureParticleBuffers(ps, pp, births)
		if births > 0 {
			ps.pendingBuf.Write(utils.ToBytesSlice(p.Newborns.Data[pp.Newborns.First:][:births]), 0)
		}
		ps.indirectBuf.Write(utils.ToBytes(&indirectCmd{
			indexCount: r.GeometryStore.IndexCountAt(pp.Geometry.Slot),
			firstIndex: r.GeometryStore.IndexBaseAt(pp.Geometry.Slot),
		}), 0)
	}

	r.retireUnusedParticles(st)
}

// buildOverlay composes this frame's overlay from everything that draws into it: the
// FPS HUD and the console, in that order (the console panel covers the HUD when open).
func (r *Renderer) buildOverlay() {
	if !r.overlayActive() {
		return
	}

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
		r.overlay.text(fmt.Sprintf("FPS %.0f", r.profiler.FPS()), x, y, size, r.fontColor)
		r.overlay.text(fmt.Sprintf("CPU %.2f ms", ms(r.profiler.CPUTime())), x, y+step, size, r.fontColor)
		if gpuTime := r.profiler.GPUTime(); gpuTime > 0 {
			r.overlay.text(fmt.Sprintf("GPU %.2f ms", ms(gpuTime)), x, y+2*step, size, r.fontColor)
			// The per-pass breakdown is the half of this worth reading: a total says
			// the budget is gone, the split says which pass spent it.
			row := 3
			for _, p := range []GPUPass{GPUPassCull, GPUPassShadow, GPUPassPrepass, GPUPassForward, GPUPassPostProcessing} {
				if !r.profiler.IsPassRecorded(p) {
					continue
				}
				// A pass that ran but measured zero is not the same as one that did not
				// run, and hiding it says the wrong thing (see Profiler.IsPassRecorded). Plain
				// ASCII, because the HUD font is a hand-drawn bitmap with no dashes to
				// spare.
				line := fmt.Sprintf("  %-8s  n/a", p)
				if d := r.profiler.PassTime(p); d > 0 {
					line = fmt.Sprintf("  %-8s %.2f", p, ms(d))
				}
				r.overlay.text(line, x, y+float32(row)*step, size, r.fontColor)
				row++
			}
		}
	}
	if r.console != nil {
		r.console.Draw(consolePainter{r.overlay}, float32(r.width), float32(r.height))
	}
}

// ---------------------------------------------------------------------------------------
// 5. Encode
// ---------------------------------------------------------------------------------------

// acquireTarget returns the texture this frame draws into. For a window that means
// blocking on vsync for the next swapchain image; it happens this late deliberately, so
// everything before it overlaps the display's flip.
func (r *Renderer) acquireTarget() gpu.Texture {
	if r.hasTarget {
		return r.target
	}
	r.profiler.beginWait(WaitAcquire)
	target, _ := r.backend.AcquireNext(r.swapchain)
	r.profiler.endWait(WaitAcquire)
	return target
}

// uploadSharedResources stages the resources every source shares — geometry streams and
// descriptors, material records — into device memory. The uploader records the copies
// into the head of the frame's command buffer and barriers them against the passes that
// read them, so a frame where nothing shared changed stages nothing and skips even the
// barrier.
func (r *Renderer) uploadSharedResources(cmd gpu.CommandBuffer) {
	r.uploader.Begin(cmd)
	r.GeometryStore.Sync(r.uploader)
	r.MaterialStore.Sync(r.uploader)
	r.uploader.End(cmd)
}

// encode records the frame's GPU work: everything from culling to the finished image in
// the target.
func (r *Renderer) encode(st *renderState, views frameViews, target gpu.Texture, cmd gpu.CommandBuffer) {
	r.profiler.beginGPUFrame(cmd)

	// 5a. The scene is shaded into the HDR scene image, or without HDR straight into the
	// target. Tell the frame steps so, then run the ones that go before everything else.
	sceneImage := target
	if r.hdr {
		sceneImage = r.sceneColor
	}
	r.prepareStepFrame(st, views, sceneImage)
	r.encodeFrameSteps(FrameStageStart, cmd)

	// 5b. Compute: skin, cull every view, simulate particles.
	r.encodeCompute(st, views, cmd)

	// 5c. Fill the shadow maps.
	r.encodeShadowPasses(st, views.shadows, cmd)
	r.encodeFrameSteps(FrameStageAfterShadows, cmd)

	// 5d. Without a view there is no scene to draw. The target is still cleared, so the
	// overlay has a defined frame to go on.
	if !views.hasMainView {
		r.encodeTargetClear(target, cmd)
		return
	}

	// 5e. A debug view replaces shading entirely, so nothing after it runs. It draws
	// straight into the target: its values are data, not light, and neither the
	// post-processing chain nor tone mapping should touch them.
	if r.debugViewActive() {
		r.encodeDebugView(st, views, target, cmd)
		return
	}

	// 5f. Fill the volumetric fog's volume, which shading reads, from the lights and the
	// shadow maps just filled.
	r.encodeVolumetricFog(st, views, cmd)

	// 5g. Fill depth first, if enabled, so shading runs once per pixel.
	depthFilled := r.encodeDepthPrepass(st, views.main, cmd)
	r.encodeFrameSteps(FrameStageAfterDepth, cmd)

	// 5h. Shade. Without HDR that is the finished image. With it, the post-processing
	// chain samples the scene image, and may sample its depth.
	r.encodeDrawingPass(st, views, depthFilled, sceneImage, cmd)
	if !r.hdr {
		return
	}
	cmd.Barrier(gpu.StageColorOutput|gpu.StageDepth, gpu.StageFragment, 0)

	// 5i. Run the post-processing chain over the scene image, then tone-map the result
	// into the target.
	r.profiler.beginPass(GPUPassPostProcessing, cmd)
	image := r.encodePostProcessing(cmd)
	r.encodeToneMapping(image, target, cmd)
	r.profiler.endPass(GPUPassPostProcessing, cmd)
}

// prepareStepFrame fills in what every stage of this frame tells its steps. image is
// what the scene is shaded into: the HDR scene image, or the target itself.
func (r *Renderer) prepareStepFrame(st *renderState, views frameViews, image gpu.Texture) {
	deltaTime := float32(0)
	if st.hasRendered {
		deltaTime = r.frame.Time - st.previousTime
	}
	st.previousTime, st.hasRendered = r.frame.Time, true

	r.stepFrame = Frame{
		Number:           r.frame.Frame,
		Time:             r.frame.Time,
		DeltaTime:        deltaTime,
		Width:            r.width,
		Height:           r.height,
		SceneDepth:       r.depth,
		SceneColor:       image,
		SceneColorFormat: r.sceneFormat(),
		Backend:          r.backend,
		LinearSampler:    r.ensureLinearSampler(),
	}
	if views.hasMainView {
		r.stepFrame.ViewProj = views.main.viewProj
		r.stepFrame.InverseViewProj = views.main.viewProj.Inv()
		r.stepFrame.Eye = views.eye
	}
}

// encodeFrameSteps runs the steps added at stage, in the order they were added, each
// told only what the stage has filled. A barrier on either side orders the steps
// against the rest of the frame: before, everything drawn so far against what the
// steps read or draw over; after, whatever they wrote — by compute, by a draw, into a
// buffer or an image — against everything after them. Between their own passes, the
// steps order themselves.
func (r *Renderer) encodeFrameSteps(stage FrameStage, cmd gpu.CommandBuffer) {
	if !r.hasFrameStepsAt(stage) {
		return
	}

	frame := r.stepFrame
	if stage < FrameStageAfterDepth {
		frame.SceneDepth = gpu.Texture{}
	}
	if stage < FrameStageAfterOpaque {
		frame.SceneColor = gpu.Texture{}
		frame.SceneColorFormat = gpu.FormatUndefined
	}

	cmd.Barrier(gpu.StageColorOutput|gpu.StageDepth, gpu.StageCompute|gpu.StageFragment|gpu.StageColorOutput|gpu.StageDepth, 0)
	for _, step := range r.frameSteps[stage] {
		step.Encode(&frame, cmd)
	}
	cmd.Barrier(gpu.StageCompute|gpu.StageFragment|gpu.StageColorOutput, gpu.StageIndirect|gpu.StageVertex|gpu.StageFragment|gpu.StageCompute|gpu.StageColorOutput|gpu.StageDepth, 0)
}

// encodeCompute runs everything the frame computes before it draws. Skinning writes
// positions, the particle kernel particle records and the particle sort their draw
// order, all read by later vertex stages; the cull reads only bounds the CPU supplied.
// So one barrier after them all covers everything — the sort orders itself after the
// simulation it reads.
func (r *Renderer) encodeCompute(st *renderState, views frameViews, cmd gpu.CommandBuffer) {
	if len(st.layout.batches) == 0 && len(r.frame.Particles.Data) == 0 {
		return
	}

	r.profiler.beginPass(GPUPassCull, cmd)
	r.encodeSkinning(st, cmd)
	r.encodeCulling(st, views, cmd)
	r.encodeParticleSimulation(st, cmd)
	r.encodeParticleSorting(st, views, cmd)
	cmd.Barrier(gpu.StageCompute, gpu.StageIndirect|gpu.StageVertex, 0)
	r.profiler.endPass(GPUPassCull, cmd)
}

// encodeSkinning dispatches one compute-skinning job per skinned mesh, sized to its
// vertex count (see scene_skin.comp). Every skinned mesh is skinned whether visible or
// not — a v1 simplification that a scene with many off-screen characters pays for.
func (r *Renderer) encodeSkinning(st *renderState, cmd gpu.CommandBuffer) {
	jobs := r.skinCommands(&r.frame)
	if len(jobs) == 0 {
		return
	}

	cmd.SetPipeline(r.skinPipeline)
	for _, job := range jobs {
		root := skinRoot{
			pos:         r.GeometryStore.PositionsAddr(),
			attr:        r.GeometryStore.AttributesAddr(),
			skin:        r.GeometryStore.SkinAddr(),
			descs:       r.GeometryStore.DescriptorsAddr(),
			joints:      st.jointBuf.Addr,
			srcDesc:     job.srcDesc,
			dstDesc:     job.dstDesc,
			jointBase:   job.jointBase,
			vertexCount: job.vertexCount,
		}
		cmd.Dispatch(utils.ToBytes(&root), (job.vertexCount+63)/64, 1, 1)
	}
}

// skinCommands builds this frame's skinning jobs, one per skinned mesh, into a scratch
// slice reused across frames.
func (r *Renderer) skinCommands(p *scenes.FramePacket) []skinCmd {
	r.skinScratch = r.skinScratch[:0]
	for _, skin := range p.Skins.Data {
		r.skinScratch = append(r.skinScratch, skinCmd{
			srcDesc: skin.Source.Slot, dstDesc: skin.Output.Slot,
			jointBase: skin.Joints.First, vertexCount: skin.VertexCount,
		})
	}
	return r.skinScratch
}

// encodeCulling culls every view: the main camera and each shadow camera.
func (r *Renderer) encodeCulling(st *renderState, views frameViews, cmd gpu.CommandBuffer) {
	if len(st.layout.batches) == 0 || !views.hasMainView {
		return
	}

	r.encodeCull(views.main, views.eye, st, cmd)
	for _, v := range views.shadows {
		r.encodeCull(v, views.eye, st, cmd)
	}
}

// encodeCull frustum-culls every drawable for one view: it resets the view's indirect
// commands from the layout's template, then dispatches one thread per drawable, which
// counts each survivor into its batch and appends it to the batch's visible region.
//
// eye is the main camera's, whichever view is culling: LOD is a main-camera decision.
// No barrier — the caller puts every view behind one.
func (r *Renderer) encodeCull(v view, eye glm.Vec3f, st *renderState, cmd gpu.CommandBuffer) {
	r.ensureCullBuffers(v.cull, &st.layout)
	v.cull.indirectBuf.Write(utils.ToBytesSlice(st.layout.template), 0)

	var castersOnly uint32
	if v.castersOnly {
		castersOnly = 1
	}
	count := uint32(len(st.layout.drawables))
	root := cullRoot{
		drawables:   st.drawableBuf.Addr,
		models:      st.worldBuf.Addr,
		indirect:    v.cull.indirectBuf.Addr,
		visible:     v.cull.visibleBuf.Addr,
		lods:        st.lodTableBuf.Addr,
		prevLevel:   st.prevLevelBuf.Addr,
		eye:         glm.Vec4f{eye[0], eye[1], eye[2], 1},
		count:       count,
		castersOnly: castersOnly,
		planes:      v.planes,
	}
	cmd.SetPipeline(r.cullPipeline)
	cmd.Dispatch(utils.ToBytes(&root), (count+63)/64, 1, 1)
}

// encodeParticleSimulation steps every particle system. Each kernel compacts the
// survivors — plus the newborns it consumes from its tail range — into the other half of
// the system's ping-pong pair, counting them into the indirect command drawParticles
// reads. See particle_update.comp for why it runs capacity+births threads.
func (r *Renderer) encodeParticleSimulation(st *renderState, cmd gpu.CommandBuffer) {
	for _, pp := range r.frame.Particles.Data {
		births := pp.Newborns.Count
		if pp.Capacity == 0 && births == 0 {
			continue
		}

		ps := st.particles[pp.ID]
		var pendingAddr uint64
		if births > 0 {
			pendingAddr = ps.pendingBuf.Addr
		}
		root := particleUpdateRoot{
			src: ps.buffers[ps.current].Addr, dst: ps.buffers[1-ps.current].Addr,
			pending: pendingAddr, indirect: ps.indirectBuf.Addr,
			capacity: pp.Capacity, pendingCount: births, dt: pp.DT,
			gravity: pp.Update.Gravity, drag: pp.Update.Drag,
		}
		if pp.Update.SizeOverLife.Enabled {
			root.sizeEnabled, root.sizeStart, root.sizeEnd = 1, pp.Update.SizeOverLife.Start, pp.Update.SizeOverLife.End
		}
		if pp.Update.OpacityOverLife.Enabled {
			root.opacityEnabled, root.opacityStart, root.opacityEnd = 1, pp.Update.OpacityOverLife.Start, pp.Update.OpacityOverLife.End
		}

		cmd.SetPipeline(r.particleUpdatePipeline)
		cmd.Dispatch(utils.ToBytes(&root), (pp.Capacity+births+63)/64, 1, 1)
		ps.current = 1 - ps.current
	}
}

// encodeParticleSorting sorts each back-to-front container's particles for the main
// camera, farthest first by depth along its view direction, into the container's order
// buffer, which the draw then reads instead of taking the particles in compaction
// order.
//
// It is a bitonic sort: one pass writes a key per entry, then log2(n)·(log2(n)+1)/2
// compare-and-swap steps order them — 55 for a thousand particles — each a dispatch of
// its own, ordered by a barrier.
//
// TODO: sort each workgroup's share in shared memory first; every step with a partner
// inside the workgroup could then run in one dispatch, which would cut the step count
// to a handful.
func (r *Renderer) encodeParticleSorting(st *renderState, views frameViews, cmd gpu.CommandBuffer) {
	if !views.hasMainView {
		return
	}
	depthPlane := viewDepthPlane(r.frame.Views[0].View)
	for _, pp := range r.frame.Particles.Data {
		ps, ok := st.particles[pp.ID]
		if pp.Sort != scenes.ParticleSortBackToFront || !ok || !ps.ready || pp.Capacity == 0 {
			continue
		}

		// The keys read the positions and the living count the simulation just wrote.
		cmd.Barrier(gpu.StageCompute, gpu.StageCompute, 0)
		count := sortEntryCount(pp.Capacity)
		keys := particleSortKeysRoot{
			particles:   ps.buffers[ps.current].Addr,
			indirect:    ps.indirectBuf.Addr,
			models:      st.worldBuf.Addr,
			order:       ps.orderBuf.Addr,
			depthPlane:  depthPlane,
			transformID: pp.Transform,
			count:       count,
		}
		cmd.SetPipeline(r.particleSortKeysPipeline)
		cmd.Dispatch(utils.ToBytes(&keys), (count+63)/64, 1, 1)

		cmd.SetPipeline(r.particleSortStepPipeline)
		for blockSize := uint32(2); blockSize <= count; blockSize *= 2 {
			for partnerDistance := blockSize / 2; partnerDistance > 0; partnerDistance /= 2 {
				cmd.Barrier(gpu.StageCompute, gpu.StageCompute, 0)
				step := particleSortStepRoot{order: ps.orderBuf.Addr, count: count, blockSize: blockSize, partnerDistance: partnerDistance}
				cmd.Dispatch(utils.ToBytes(&step), (count+63)/64, 1, 1)
			}
		}
	}
}

// viewDepthPlane returns the plane whose signed distance is a point's depth along the
// view direction: the view matrix's third row, negated, since a camera looks down its
// view space's -z.
func viewDepthPlane(view glm.Mat4f) glm.Vec4f {
	return view.Row(2).Scale(-1)
}

// encodeShadowPasses fills every shadow map, then hands each to the samplers.
//
// Views sharing a map arrive consecutively, each group starting with the view that
// clears it (see collectShadowViews), so a group is the run from one clearing view up to
// the next — and a group is drawn in a single render pass.
func (r *Renderer) encodeShadowPasses(st *renderState, shadows []view, cmd gpu.CommandBuffer) {
	if len(shadows) == 0 || len(st.layout.batches) == 0 {
		return
	}

	r.profiler.beginPass(GPUPassShadow, cmd)
	for start := 0; start < len(shadows); {
		end := start + 1
		for end < len(shadows) && !shadows[end].clearsMap {
			end++
		}
		r.encodeShadowMap(shadows[start:end], st, cmd)
		start = end
	}
	// Every map is written before anything samples one, so one barrier covers them all:
	// lit shaders, and the volumetric fog's compute pass.
	cmd.Barrier(gpu.StageDepth, gpu.StageFragment|gpu.StageCompute, 0)
	r.profiler.endPass(GPUPassShadow, cmd)
}

// encodeShadowMap fills one shadow map from every view that shares it, in one render
// pass: each confines itself to its own region with the viewport and scissor, and only
// the first clears. A pass apiece would load and store the whole atlas per cascade for
// nothing.
func (r *Renderer) encodeShadowMap(group []view, st *renderState, cmd gpu.CommandBuffer) {
	cmd.BeginRenderPass(gpu.RenderTargets{
		Depth: &gpu.DepthAttachment{
			Texture: r.TextureStore.GPU(group[0].shadowMap),
			Load:    gpu.LoadClear, Store: gpu.StoreKeep, Clear: 0.0,
		},
	})
	cmd.SetPipeline(r.shadowPipeline)

	for _, v := range group {
		root := r.positionRoot(v, st)
		cmd.SetViewport(float32(v.x), 0, float32(v.width), float32(v.height), 0, 1)
		cmd.SetScissor(v.x, 0, int32(v.width), int32(v.height))
		cmd.DrawIndexedIndirect(utils.ToBytes(&root), r.GeometryStore.IndexBuffer(), gpu.IndexUint32, v.cull.indirectBuf, 0, uint32(len(st.layout.batches)), indirectSize)
	}

	cmd.EndRenderPass()
}

// encodeTargetClear clears the target to the clear colour, for a frame with no view to
// draw.
func (r *Renderer) encodeTargetClear(target gpu.Texture, cmd gpu.CommandBuffer) {
	cmd.BeginRenderPass(gpu.RenderTargets{
		Color: []gpu.ColorAttachment{{Texture: target, Load: gpu.LoadClear, Store: gpu.StoreKeep, Clear: [4]float32(r.clear)}},
	})
	cmd.EndRenderPass()
}

// encodeDebugView draws every visible batch through the active view's pipeline, in one
// multi-draw-indirect call, in place of shading.
//
// One call rather than one per raster span (the way drawBatches does it) because every
// batch goes through the same pipeline here regardless of its material — which is also
// why one root serves them all: the surface views read no material record, and the id
// views read positions only.
//
// The overlay pass that follows draws the console on top, so it stays usable while a
// view is up — which is the only way to turn one off again.
func (r *Renderer) encodeDebugView(st *renderState, views frameViews, target gpu.Texture, cmd gpu.CommandBuffer) {
	cmd.BeginRenderPass(gpu.RenderTargets{
		Color: []gpu.ColorAttachment{{Texture: target, Load: gpu.LoadClear, Store: gpu.StoreKeep, Clear: debugClear(r.debugView, r.clear)}},
		Depth: &gpu.DepthAttachment{Texture: r.depth, Load: gpu.LoadClear, Store: gpu.StoreKeep, Clear: 0.0},
	})
	cmd.SetViewport(0, 0, float32(r.width), float32(r.height), 0, 1)
	cmd.SetScissor(0, 0, int32(r.width), int32(r.height))

	if len(st.layout.batches) > 0 {
		var root []byte
		if r.debugSurfaceView() {
			sceneRoot := r.sceneRoot(st, views)
			root = utils.ToBytes(&sceneRoot)
		} else {
			positionRoot := r.positionRoot(views.main, st)
			root = utils.ToBytes(&positionRoot)
		}
		cmd.SetPipeline(r.debugPipelineFor(r.debugView))
		cmd.DrawIndexedIndirect(root, r.GeometryStore.IndexBuffer(), gpu.IndexUint32, views.main.cull.indirectBuf, 0, uint32(len(st.layout.batches)), indirectSize)
	}

	cmd.EndRenderPass()
}

// encodeVolumetricFog fills the fog volume for a scene whose fog is volumetric, in two
// compute passes. The first writes, for every froxel, how dense the fog is there and
// how much light it sends toward the camera; the second accumulates that along each
// column, from the camera out, into what the fog between the camera and each froxel
// adds and lets through — which lit shaders then look up (see applyFog).
func (r *Renderer) encodeVolumetricFog(st *renderState, views frameViews, cmd gpu.CommandBuffer) {
	fog := r.frame.Environment.Fog
	if fog.Mode != scenes.FogVolumetric {
		return
	}

	size := r.volumetricFog.resolved()
	extent := [3]uint32{size.Width, size.Height, size.Depth}
	inject := fogInjectRoot{
		inverseViewProj: r.stepFrame.InverseViewProj,
		lights:          st.lights.Addr(),
		eye:             views.eye,
		density:         fog.Density,
		albedo:          glm.Vec3f(fog.Albedo),
		anisotropy:      fog.Anisotropy,
		emission:        glm.Vec3f(fog.Emission),
		baseHeight:      fog.BaseHeight,
		heightFalloff:   fog.HeightFalloff,
		reach:           fog.Reach,
		medium:          r.fogMedium.Index,
		shadowSampler:   r.shadowSampler.Index,
		size:            extent,
	}
	cmd.SetPipeline(r.fogInjectPipeline)
	cmd.Dispatch(utils.ToBytes(&inject), (size.Width+3)/4, (size.Height+3)/4, (size.Depth+3)/4)
	cmd.Barrier(gpu.StageCompute, gpu.StageCompute, 0)

	integrate := fogIntegrateRoot{
		medium:  r.fogMedium.Index,
		volume:  r.fogVolume.Index,
		sampler: r.ensureLinearSampler().Index,
		reach:   fog.Reach,
		size:    extent,
	}
	cmd.SetPipeline(r.fogIntegratePipeline)
	cmd.Dispatch(utils.ToBytes(&integrate), (size.Width+7)/8, (size.Height+7)/8, 1)
	cmd.Barrier(gpu.StageCompute, gpu.StageFragment, 0)
}

// encodeDepthPrepass fills depth for the opaque geometry in a pass of its own, and
// reports whether it did — in which case the drawing pass loads that depth rather than
// clearing it.
//
// Its own pass, with no colour attachment, because that is what makes it depth-only. A
// pipeline sharing the drawing pass would have to declare that pass's colour attachment,
// and a fragment stage that writes nothing to a declared attachment does not skip the
// work — it writes an undefined value, corrupting the target and paying full-rate
// fragment cost over every opaque surface.
//
// Only opaque batches take part. A blended surface does not occlude what is behind it,
// so writing its depth here would hide geometry that should show through.
//
// Frame steps at FrameStageAfterDepth read the depth this pass fills, so adding one
// turns the pass on. It then runs even with nothing to draw: its clear is what keeps
// those steps from reading the previous frame's depth.
func (r *Renderer) encodeDepthPrepass(st *renderState, main view, cmd gpu.CommandBuffer) bool {
	if !r.hasFrameStepsAt(FrameStageAfterDepth) && (!r.depthPrepass || len(st.layout.batches) == 0) {
		return false
	}

	r.profiler.beginPass(GPUPassPrepass, cmd)
	cmd.BeginRenderPass(gpu.RenderTargets{
		Depth: &gpu.DepthAttachment{Texture: r.depth, Load: gpu.LoadClear, Store: gpu.StoreKeep, Clear: 0.0},
	})
	cmd.SetViewport(0, 0, float32(r.width), float32(r.height), 0, 1)
	cmd.SetScissor(0, 0, int32(r.width), int32(r.height))

	// One root serves every span: this pass reads positions and transforms, nothing that
	// varies by material.
	root := r.positionRoot(main, st)
	drew := false
	boundCull := materials.CullMode(255)
	for first, count := range rasterSpans(st.layout.batches) {
		b := &st.layout.batches[first]
		if b.blend != materials.BlendOpaque {
			continue
		}
		// A material with its own vertex program can put its vertices anywhere, and this
		// pass runs the standard one — so its depth would not be that material's. Those
		// spans shade as they always did, just without the prepass.
		if b.pool.Shader().Vertex != nil {
			continue
		}
		// Match the drawing pass's culling exactly, or this writes depth for faces it will
		// discard. Spans arrive grouped by raster state, so this rebinds rarely.
		if b.cull != boundCull {
			boundCull = b.cull
			cmd.SetPipeline(r.prepassPipelines[min(int(b.cull), len(r.prepassPipelines)-1)])
		}
		cmd.DrawIndexedIndirect(utils.ToBytes(&root), r.GeometryStore.IndexBuffer(), gpu.IndexUint32, main.cull.indirectBuf, uint64(first)*uint64(indirectSize), uint32(count), indirectSize)
		drew = true
	}

	cmd.EndRenderPass()
	// The drawing pass tests against this depth, and loads it when drew is set.
	cmd.Barrier(gpu.StageDepth, gpu.StageDepth, 0)
	r.profiler.endPass(GPUPassPrepass, cmd)
	return drew
}

// encodeDrawingPass shades the scene into image — the HDR scene image, or the target
// itself — in two render passes: the opaque batches, then the blended batches and the
// particles over them. Two rather than one so that the frame steps needing the
// finished opaque scene — a sky drawn behind it, a copy of it for refraction — run in
// between.
func (r *Renderer) encodeDrawingPass(st *renderState, views frameViews, depthFilled bool, image gpu.Texture, cmd gpu.CommandBuffer) {
	opaque, transparent := splitByBlend(st.layout.batches)

	r.profiler.beginPass(GPUPassForward, cmd)
	r.encodeOpaquePass(opaque, st, views, depthFilled, image, cmd)
	r.encodeFrameSteps(FrameStageAfterOpaque, cmd)
	r.encodeFogBackground(image, cmd)
	r.encodeTransparentPass(transparent, st, views, image, cmd)
	r.encodeFrameSteps(FrameStageAfterTransparent, cmd)
	r.profiler.endPass(GPUPassForward, cmd)
}

// encodeOpaquePass clears image and draws the opaque batches into it. It clears depth
// unless the prepass already filled it.
func (r *Renderer) encodeOpaquePass(batches batchRange, st *renderState, views frameViews, depthFilled bool, image gpu.Texture, cmd gpu.CommandBuffer) {
	depthLoad := gpu.LoadClear
	if depthFilled {
		depthLoad = gpu.LoadKeep
	}

	cmd.BeginRenderPass(gpu.RenderTargets{
		Color: []gpu.ColorAttachment{{Texture: image, Load: gpu.LoadClear, Store: gpu.StoreKeep, Clear: [4]float32(r.clear)}},
		Depth: &gpu.DepthAttachment{Texture: r.depth, Load: depthLoad, Store: gpu.StoreKeep, Clear: 0.0},
	})
	cmd.SetViewport(0, 0, float32(r.width), float32(r.height), 0, 1)
	cmd.SetScissor(0, 0, int32(r.width), int32(r.height))

	r.drawBatches(batches, st, views, cmd)

	cmd.EndRenderPass()
}

// encodeFogBackground fogs the pixels no geometry covers, for a scene whose fog is
// volumetric: no lit shader runs for them, so no applyFog reaches them, and without it
// distant geometry would fade into fog against a background that stays clear. They take
// the fog out to the volume's reach.
//
// It runs after the steps that draw behind the opaque scene — a sky — so the fog lies
// over them as well, and before anything blended, which fogs itself.
func (r *Renderer) encodeFogBackground(image gpu.Texture, cmd gpu.CommandBuffer) {
	if r.frame.Environment.Fog.Mode != scenes.FogVolumetric {
		return
	}

	pass := r.ensureFogBackgroundPass()
	// It tests against the depth the opaque pass wrote, and blends over what it, or a
	// step, drew.
	cmd.Barrier(gpu.StageColorOutput|gpu.StageDepth, gpu.StageDepth|gpu.StageColorOutput, 0)
	frame := r.postProcessingFrame(image, image)
	params := fogBackgroundParams{
		volume:    r.fogVolume.Index,
		lastSlice: 1 - 0.5/float32(r.volumetricFog.resolved().Depth),
	}
	pass.DrawWithDepth(image, r.depth, r.width, r.height, gpu.LoadKeep, frame.Root(image, r.width, r.height).With(utils.ToBytes(&params)), cmd)
}

// encodeTransparentPass draws the blended batches, back to front, then the particles,
// over the opaque scene in image, testing against the depth it left. A frame with neither skips the
// pass: ending one render pass and starting another stores both attachments and loads
// them back, which is a cost for nothing when nothing is drawn.
func (r *Renderer) encodeTransparentPass(batches batchRange, st *renderState, views frameViews, image gpu.Texture, cmd gpu.CommandBuffer) {
	if batches.isEmpty() && len(r.frame.Particles.Data) == 0 {
		return
	}

	// Blending reads what the opaque pass wrote, and the depth test its depth.
	cmd.Barrier(gpu.StageColorOutput|gpu.StageDepth, gpu.StageColorOutput|gpu.StageDepth, 0)
	cmd.BeginRenderPass(gpu.RenderTargets{
		Color: []gpu.ColorAttachment{{Texture: image, Load: gpu.LoadKeep, Store: gpu.StoreKeep}},
		Depth: &gpu.DepthAttachment{Texture: r.depth, Load: gpu.LoadKeep, Store: gpu.StoreKeep},
	})
	cmd.SetViewport(0, 0, float32(r.width), float32(r.height), 0, 1)
	cmd.SetScissor(0, 0, int32(r.width), int32(r.height))

	r.drawBackToFront(batches, st, views, cmd)
	r.drawParticles(st, views, cmd)

	cmd.EndRenderPass()
}

// drawBatches issues one multi-draw-indirect call per raster span in batches: every
// per-geometry command sharing a pipeline goes in a single DrawIndexedIndirect. Each
// command's firstInstance is its region base, so gl_InstanceIndex indexes the compacted
// visible buffer directly.
func (r *Renderer) drawBatches(batches batchRange, st *renderState, views frameViews, cmd gpu.CommandBuffer) {
	root := r.sceneRoot(st, views)
	for offset, count := range rasterSpans(st.layout.batches[batches.first:batches.end]) {
		first := batches.first + offset
		b := &st.layout.batches[first]
		root.materials = b.pool.RecordsAddr()
		cmd.SetPipeline(r.drawPipelines[r.pipelineForPool(b.pool, b.cull, b.blend)])
		cmd.DrawIndexedIndirect(utils.ToBytes(&root), r.GeometryStore.IndexBuffer(), gpu.IndexUint32, views.main.cull.indirectBuf, uint64(first)*uint64(indirectSize), uint32(count), indirectSize)
	}
}

// drawBackToFront issues the blended batches in batches farthest first, one indirect
// draw each: a blended mesh is a batch of its own, and the batches are sorted every
// frame, since the order changes whenever the camera or a mesh moves. The layout is
// built once and never reordered; only the order the draws are issued in changes.
//
// TODO: a double-sided blended mesh overlaps itself — a jar's back wall is behind its
// front wall, in the same draw — and which is drawn first is up to triangle order. Draw
// such a batch twice, back faces first (cull front), then front faces (cull back).
func (r *Renderer) drawBackToFront(batches batchRange, st *renderState, views frameViews, cmd gpu.CommandBuffer) {
	st.backToFront = sortBackToFront(batches, &st.layout, &r.frame, views.eye, st.backToFront)

	root := r.sceneRoot(st, views)
	boundPipeline := ^uint32(0)
	for _, entry := range st.backToFront {
		b := &st.layout.batches[entry.batch]
		if pipeline := r.pipelineForPool(b.pool, b.cull, b.blend); pipeline != boundPipeline {
			cmd.SetPipeline(r.drawPipelines[pipeline])
			boundPipeline = pipeline
		}
		root.materials = b.pool.RecordsAddr()
		cmd.DrawIndexedIndirect(utils.ToBytes(&root), r.GeometryStore.IndexBuffer(), gpu.IndexUint32, views.main.cull.indirectBuf, uint64(entry.batch)*uint64(indirectSize), 1, indirectSize)
	}
}

// drawParticles issues one indirect draw per particle system, reading the instance count
// its simulation kernel just compacted. Particle systems are never batched: each is one
// geometry and one material already.
func (r *Renderer) drawParticles(st *renderState, views frameViews, cmd gpu.CommandBuffer) {
	for _, pp := range r.frame.Particles.Data {
		ps, ok := st.particles[pp.ID]
		if !ok || !ps.ready {
			continue
		}

		pool := r.MaterialStore.PoolAt(pp.Material.PoolID)
		root := particleDrawRoot{
			viewProj:    views.main.viewProj,
			pos:         r.GeometryStore.PositionsAddr(),
			attr:        r.GeometryStore.AttributesAddr(),
			descs:       r.GeometryStore.DescriptorsAddr(),
			models:      st.worldBuf.Addr,
			particles:   ps.buffers[ps.current].Addr,
			materials:   pool.RecordsAddr(),
			lights:      st.lights.Addr(),
			eye:         glm.Vec4f{views.eye[0], views.eye[1], views.eye[2], 1},
			geometryID:  pp.Geometry.Slot,
			materialID:  pp.Material.Slot,
			transformID: pp.Transform,
			time:        r.frame.Time,
		}
		if pp.Sort == scenes.ParticleSortBackToFront {
			root.order = ps.orderBuf.Addr
		}
		pipeline := r.pipelineForPool(pool, pool.CullAt(pp.Material.Slot), pool.BlendAt(pp.Material.Slot))
		cmd.SetPipeline(r.drawPipelines[pipeline])
		cmd.DrawIndexedIndirect(utils.ToBytes(&root), r.GeometryStore.IndexBuffer(), gpu.IndexUint32, ps.indirectBuf, 0, 1, indirectSize)
	}
}

// encodePostProcessing runs every step of the chain, in order, over the shaded scene,
// and returns the image the last one wrote. With an empty chain that is the scene image
// itself.
//
// The steps ping-pong between the scene image and a second image of the same size: each
// reads the one the step before it wrote, and writes the other — so between two steps,
// what one wrote must be ordered before the next samples it, and what one sampled
// before the next overwrites it.
func (r *Renderer) encodePostProcessing(cmd gpu.CommandBuffer) gpu.Texture {
	if len(r.postProcessing) == 0 {
		return r.sceneColor
	}
	if !r.postColor.IsValid() {
		r.postColor = postprocess.CreateImage(r.backend, r.width, r.height, "post-color")
	}

	image := r.sceneColor
	for _, step := range r.postProcessing {
		next := r.postColor
		if image == r.postColor {
			next = r.sceneColor
		}
		frame := r.postProcessingFrame(image, next)
		step.Encode(&frame, cmd)
		cmd.Barrier(gpu.StageColorOutput|gpu.StageFragment, gpu.StageFragment|gpu.StageColorOutput, 0)
		image = next
	}
	return image
}

// encodeToneMapping maps the finished HDR image into the target: exposed, and through
// the tone-map curve. The target encodes it for display.
func (r *Renderer) encodeToneMapping(image, target gpu.Texture, cmd gpu.CommandBuffer) {
	frame := r.postProcessingFrame(image, target)
	root := toneMapRoot{
		Root:     frame.Root(image, r.width, r.height),
		operator: r.toneMapping,
		exposure: r.exposureStops,
	}
	r.toneMapPass.Draw(target, r.width, r.height, gpu.LoadClear, utils.ToBytes(&root), cmd)
}

// postProcessingFrame is what a post-processing pass reading source and writing target
// is given.
func (r *Renderer) postProcessingFrame(source, target gpu.Texture) postprocess.Frame {
	return postprocess.Frame{
		Source:          source,
		Target:          target,
		Width:           r.width,
		Height:          r.height,
		Time:            r.frame.Time,
		ViewProj:        r.stepFrame.ViewProj,
		InverseViewProj: r.stepFrame.InverseViewProj,
		Eye:             r.stepFrame.Eye,
		SceneDepth:      r.depth,
		Backend:         r.backend,
		LinearSampler:   r.ensureLinearSampler(),
	}
}

// recordScreenshot copies the frame's colour target into the readback buffer, with the
// frame's command buffer still open and the scene already drawn into target.
//
// Render calls it between the scene and the overlay, which is what keeps the overlay out
// of the image. Capturing what the scene looks like is the point — the console is the
// instrument, not the subject — and it would otherwise cover a third of the frame in
// exactly the cases where someone is capturing to inspect something.
func (r *Renderer) recordScreenshot(target gpu.Texture, cmd gpu.CommandBuffer) {
	if r.pendingShot == nil {
		return
	}
	n := int(r.width * r.height * 4)
	if !r.readback.IsValid() || len(r.pixels) < n {
		if r.readback.IsValid() {
			r.backend.Free(r.readback)
		}
		r.readback = r.backend.Alloc(uint64(n), gpu.MemoryHost, "readback")
		r.pixels = make([]byte, n)
	}
	cmd.Barrier(gpu.StageColorOutput, gpu.StageTransfer, 0)
	cmd.CopyTextureToBuffer(r.readback, target, 0, 0)
}

// ---------------------------------------------------------------------------------------
// 7. Overlay
// ---------------------------------------------------------------------------------------

// encodeOverlay draws the HUD and the console over the finished frame, in a pass of its
// own so a capture can be taken between the scene and the overlay.
//
// It is also where the frame's GPU profile closes, and the placement is load-bearing:
// the two backends disagree about what is safe, and this is the only arrangement measured
// to drop nothing on either. writeSkippedPasses goes in while the pass is open — it
// stands in for passes the frame skipped, and a backend that resolves timestamps at
// render-pass boundaries needs something to attach them to. The frame's closing
// timestamp goes in after the pass ends: written as the last command inside it,
// KosmicKrisp dropped it intermittently (24 of 40 frames), which froze the HUD.
//
// Profiling runs only while the HUD is showing, so the pass always runs when there is a
// profile to close.
func (r *Renderer) encodeOverlay(target gpu.Texture, cmd gpu.CommandBuffer) {
	if !r.overlayActive() && !r.profiler.isGPUProfilingEnabled() {
		return
	}

	// The overlay blends over whatever wrote the target last, after a screenshot has
	// copied it, and loads the depth the scene wrote and the post-processing chain may
	// have sampled.
	cmd.Barrier(gpu.StageColorOutput|gpu.StageDepth|gpu.StageFragment|gpu.StageTransfer, gpu.StageColorOutput|gpu.StageDepth, 0)
	cmd.BeginRenderPass(gpu.RenderTargets{
		Color: []gpu.ColorAttachment{{Texture: target, Load: gpu.LoadKeep, Store: gpu.StoreKeep}},
		Depth: &gpu.DepthAttachment{Texture: r.depth, Load: gpu.LoadKeep, Store: gpu.StoreKeep},
	})
	cmd.SetViewport(0, 0, float32(r.width), float32(r.height), 0, 1)
	cmd.SetScissor(0, 0, int32(r.width), int32(r.height))
	if r.overlay != nil {
		r.overlay.draw(cmd, float32(r.width), float32(r.height))
	}
	r.profiler.writeSkippedPasses(cmd)
	cmd.EndRenderPass()

	r.profiler.endGPUFrame(cmd)
}

// ---------------------------------------------------------------------------------------
// 8. Submit
// ---------------------------------------------------------------------------------------

// submit hands the frame to the GPU and blocks until it is done: presented, for a
// window, or finished, for an offscreen target — whose Capture submits its own readback
// on the same queue afterwards and so sees the result.
func (r *Renderer) submit(cmd gpu.CommandBuffer) {
	r.profiler.beginWait(WaitSubmit)
	if r.hasTarget {
		r.backend.Wait(r.backend.Submit(cmd))
	} else {
		r.backend.Present(r.swapchain, cmd)
	}
	r.profiler.endWait(WaitSubmit)
}

// finishFrame does what needs the frame to be complete. Both submit paths drain the
// queue, so a screenshot's readback holds finished pixels and the GPU timestamps are
// ready to read.
func (r *Renderer) finishFrame() {
	r.writeScreenshot()
	r.profiler.readGPUTimestamps(r.backend)
	r.profiler.endFrame()
}

// writeScreenshot encodes and writes the captured frame. Called after the frame has
// been submitted and drained, so the readback buffer holds finished pixels.
func (r *Renderer) writeScreenshot() {
	shot := r.pendingShot
	if shot == nil {
		return
	}
	r.pendingShot = nil

	err := r.encodePNG(shot.path)
	if shot.done != nil {
		shot.done(shot.path, err)
	}
}

// encodePNG converts the readback buffer to an image and writes it.
func (r *Renderer) encodePNG(path string) error {
	n := int(r.width * r.height * 4)
	if !r.readback.IsValid() || len(r.pixels) < n {
		return fmt.Errorf("no frame was captured (the target may not allow readback)")
	}
	copy(r.pixels, unsafe.Slice((*byte)(r.readback.Ptr), n))

	img := image.NewRGBA(image.Rect(0, 0, int(r.width), int(r.height)))
	copy(img.Pix, r.pixels[:n])
	// A swapchain is commonly BGRA while image.RGBA is, unsurprisingly, RGBA — without
	// this the sky comes out orange.
	if r.color == gpu.FormatBGRA8Unorm || r.color == gpu.FormatBGRA8Srgb {
		for i := 0; i+3 < len(img.Pix); i += 4 {
			img.Pix[i], img.Pix[i+2] = img.Pix[i+2], img.Pix[i]
		}
	}
	// The frame is opaque; whatever alpha the target ended up with is not meaningful
	// here, and a stray 0 would make the PNG transparent.
	for i := 3; i < len(img.Pix); i += 4 {
		img.Pix[i] = 0xFF
	}

	if dir := filepath.Dir(path); dir != "." {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return err
		}
	}
	f, err := os.Create(path)
	if err != nil {
		return err
	}
	defer f.Close()
	return png.Encode(f, img)
}

// ---------------------------------------------------------------------------------------
// Roots, resources and lookups the steps above share
// ---------------------------------------------------------------------------------------

// sceneRoot is the root every pass over the scene vertex stage pushes this frame, with
// no material records: drawBatches fills those per span, and a debug fragment shader
// never reads them.
func (r *Renderer) sceneRoot(st *renderState, views frameViews) drawRoot {
	return drawRoot{
		viewProj:      views.main.viewProj,
		pos:           r.GeometryStore.PositionsAddr(),
		attr:          r.GeometryStore.AttributesAddr(),
		descs:         r.GeometryStore.DescriptorsAddr(),
		models:        st.worldBuf.Addr,
		drawables:     st.drawableBuf.Addr,
		visible:       views.main.cull.visibleBuf.Addr,
		lights:        st.lights.Addr(),
		eye:           glm.Vec4f{views.eye[0], views.eye[1], views.eye[2], 1},
		shadowSampler: r.shadowSampler.Index,
		time:          r.frame.Time,
	}
}

// positionRoot is the root of a position-only pass rendering one view: the shadow
// passes, the depth prepass and the id debug views.
func (r *Renderer) positionRoot(v view, st *renderState) positionRoot {
	return positionRoot{
		viewProj:  v.viewProj,
		pos:       r.GeometryStore.PositionsAddr(),
		descs:     r.GeometryStore.DescriptorsAddr(),
		models:    st.worldBuf.Addr,
		drawables: st.drawableBuf.Addr,
		visible:   v.cull.visibleBuf.Addr,
	}
}

// flipClipY negates clip-space Y (proj[1][1]*=-1) so images are right-side-up under
// Vulkan's Y-down NDC. Only screen position is affected.
func flipClipY(m glm.Mat4f) glm.Mat4f {
	m[1], m[5], m[9], m[13] = -m[1], -m[5], -m[9], -m[13]
	return m
}

// growBuffer makes sure buf holds at least size bytes. Grow-only: a buffer already large
// enough is kept, so a steady frame allocates nothing, and one that must grow at least
// doubles, so a scene growing a little every frame does not reallocate every frame.
// Every writer bounds itself by the current count rather than the buffer's size, so the
// slack is harmless.
func (r *Renderer) growBuffer(buf *gpu.Buffer, size uint64, label string) {
	size = max(size, 1)
	if buf.IsValid() && buf.Size >= size {
		return
	}
	if buf.IsValid() {
		size = max(size, buf.Size*2)
		r.backend.Free(*buf)
	}
	*buf = r.backend.Alloc(size, gpu.MemoryHost, label)
}

// ensureCullBuffers sizes one view's cull buffers for the current layout.
func (r *Renderer) ensureCullBuffers(cull *cullBuffers, layout *drawLayout) {
	r.growBuffer(&cull.indirectBuf, uint64(max(len(layout.batches), 1))*uint64(indirectSize), "indirect")
	r.growBuffer(&cull.visibleBuf, uint64(layout.visibleCapacity)*4, "visible")
}

// ensureShadowSampler creates the PCF comparison sampler every lit shader samples shadow
// maps with, on first use.
func (r *Renderer) ensureShadowSampler() {
	if r.shadowSampler.H != 0 {
		return
	}
	r.shadowSampler = r.backend.CreateSampler(gpu.SamplerDescriptor{
		MinLinear: true, MagLinear: true,
		AddressU: gpu.AddressClamp, AddressV: gpu.AddressClamp,
		Compare: gpu.CompareGreaterEqual, // reversed-Z: nearer is greater
		Label:   "shadow-cmp",
	})
}

// ensureFogVolumes creates volumetric fog's two volumes, at the size the settings ask
// for, on first use, and returns that size: the medium the inject pass writes, and the
// fog the integrate pass accumulates from it. Half floats hold light above 1.0 and a
// transmittance fine enough not to band.
func (r *Renderer) ensureFogVolumes() VolumetricFogSettings {
	size := r.volumetricFog.resolved()
	if !r.fogVolume.IsValid() {
		volume := gpu.TextureDescriptor{
			Kind: gpu.Texture3D, Width: size.Width, Height: size.Height, Depth: size.Depth,
			Format: gpu.FormatRGBA16F, Usage: gpu.TextureSampled | gpu.TextureStorage,
		}
		volume.Label = "fog-medium"
		r.fogMedium = r.backend.CreateTexture(volume)
		volume.Label = "fog-volume"
		r.fogVolume = r.backend.CreateTexture(volume)
	}
	return size
}

// releaseFogVolumes destroys volumetric fog's volumes, if any, for ensureFogVolumes to
// make again.
func (r *Renderer) releaseFogVolumes() {
	for _, t := range []*gpu.Texture{&r.fogMedium, &r.fogVolume} {
		if t.IsValid() {
			r.backend.DestroyTexture(*t)
			*t = gpu.Texture{}
		}
	}
}

// ensureFogBackgroundPass builds the pass that fogs the background, for the format the
// scene is shaded into, on first use and again whenever that format changes. Its depth
// test is what confines it to the background: the triangle lies on the far plane, so
// it passes only where depth still holds the far plane's clear value, and pixels with
// geometry are rejected before the shader runs. It composites through "over" blending,
// which every backend has (see fog_background.frag.glsl for how that comes out as the
// fog).
func (r *Renderer) ensureFogBackgroundPass() *postprocess.FullscreenPass {
	format := r.sceneFormat()
	if r.fogBackgroundPass != nil && r.fogBackgroundFormat == format {
		return r.fogBackgroundPass
	}
	if r.fogBackgroundPass != nil {
		r.fogBackgroundPass.Release()
	}
	r.fogBackgroundPass = postprocess.NewFullscreenPass(r.backend, postprocess.FullscreenPassDescriptor{
		Fragment:     shaders.ForBackend(r.backend, shaders.FogBackground),
		Format:       format,
		Blend:        []gpu.BlendState{{Enable: true, ColorOp: gpu.BlendFactorOp{Src: gpu.BlendSrcAlpha, Dst: gpu.BlendOneMinusSrcAlpha, Op: gpu.BlendAdd}}},
		DepthFormat:  gpu.FormatDepth32F,
		DepthCompare: gpu.CompareGreaterEqual,
		Label:        "fog-background",
	})
	r.fogBackgroundFormat = format
	return r.fogBackgroundPass
}

// ensureLinearSampler creates the linear, clamp-to-edge sampler every post-processing
// pass reads through, on first use.
func (r *Renderer) ensureLinearSampler() gpu.Sampler {
	if r.linearSampler.H == 0 {
		r.linearSampler = r.backend.CreateSampler(gpu.SamplerDescriptor{
			MinLinear: true, MagLinear: true,
			AddressU: gpu.AddressClamp, AddressV: gpu.AddressClamp,
			Label: "post-linear",
		})
	}
	return r.linearSampler
}

// ensureParticleBuffers allocates a system's ping-pong buffers and indirect command on
// first use — sized to its capacity, which is fixed at construction — and grows the
// newborn buffer to fit this frame's births. A fresh or cleared system's buffers are
// zeroed, so every slot starts dead (age 0 >= lifetime 0) rather than holding whatever
// was in freshly allocated memory.
func (r *Renderer) ensureParticleBuffers(ps *particleState, pp scenes.ParticlePacket, births uint32) {
	if !ps.ready || ps.epoch != pp.Epoch {
		size := uint64(pp.Capacity) * uint64(particleRecordSize)
		for i := range ps.buffers {
			r.growBuffer(&ps.buffers[i], size, "particles")
			clear(unsafe.Slice((*byte)(ps.buffers[i].Ptr), ps.buffers[i].Size))
		}
		r.growBuffer(&ps.indirectBuf, uint64(indirectSize), "particles-indirect")
		ps.current, ps.ready, ps.epoch = 0, true, pp.Epoch
	}
	if births > 0 {
		r.growBuffer(&ps.pendingBuf, uint64(births)*uint64(particleRecordSize), "particles-pending")
	}
	if pp.Sort == scenes.ParticleSortBackToFront {
		r.growBuffer(&ps.orderBuf, uint64(sortEntryCount(pp.Capacity))*sortEntrySize, "particles-order")
	}
}

// sortEntryCount is how many entries a container's order holds: its capacity rounded
// up to a power of two, which is what a bitonic sort works on.
func sortEntryCount(capacity uint32) uint32 {
	return 1 << bits.Len32(max(capacity, 1)-1)
}

// stateFor returns the GPU state for a source, creating it on first sight.
func (r *Renderer) stateFor(id scenes.SourceID) *renderState {
	if st, ok := r.sources[id]; ok {
		return st
	}
	if r.sources == nil {
		r.sources = make(map[scenes.SourceID]*renderState)
	}
	st := &renderState{lights: NewLights(r.backend)}
	r.sources[id] = st
	return st
}

// shadowResourceFor returns a light's shadow resources, creating them on first sight,
// and marks them used this frame.
func (r *Renderer) shadowResourceFor(light scenes.LightID, st *renderState) *shadowResource {
	sh, ok := st.shadows[light]
	if !ok {
		if st.shadows == nil {
			st.shadows = make(map[scenes.LightID]*shadowResource)
		}
		sh = &shadowResource{}
		st.shadows[light] = sh
	}
	sh.seen = true
	return sh
}

// retireUnusedShadows frees the resources of lights that stopped casting or went away.
// Without it a scene toggling shadows on a long-lived light would hold every depth map
// it ever allocated.
func (r *Renderer) retireUnusedShadows(st *renderState) {
	for id, sh := range st.shadows {
		if sh.seen {
			sh.seen = false
			continue
		}
		sh.destroy()
		delete(st.shadows, id)
	}
}

// particleStateFor returns a particle system's simulation state, creating it on first
// sight, and marks it used this frame.
func (r *Renderer) particleStateFor(system scenes.ParticleID, st *renderState) *particleState {
	ps, ok := st.particles[system]
	if !ok {
		if st.particles == nil {
			st.particles = make(map[scenes.ParticleID]*particleState)
		}
		ps = &particleState{}
		st.particles[system] = ps
	}
	ps.seen = true
	return ps
}

// retireUnusedParticles frees the simulations of systems that left the packet — detached
// or destroyed. Their particles go with them: the buffers are the simulation, and there
// is nothing on the CPU to restore it from.
func (r *Renderer) retireUnusedParticles(st *renderState) {
	for id, ps := range st.particles {
		if ps.seen {
			ps.seen = false
			continue
		}
		ps.destroy(r.backend)
		delete(st.particles, id)
	}
}

// --------------------------------------------------------------------------------------
// Shadow fitting
//
// Where each light's shadow cameras point and how far they reach. fitShadows calls these;
// they depend on the view being rendered, which is why they are the renderer's.
// --------------------------------------------------------------------------------------

// shadowReach is how far the shadow fit should cover, in world units from the eye, or
// zero to derive it from the view.
//
// Explicit cascade steps decide it: the outermost one is by definition where shadows
// stop, so it has to set the range rather than be clipped by a separately chosen one.
// Renderer.SetShadowDistance therefore has no effect while explicit steps are in use,
// which keeps one setting in charge of the far end instead of two disagreeing.
func (r *Renderer) shadowReach() float32 {
	if c, ok := r.Shadows().(ShadowCascaded); ok {
		if steps := c.steps(); len(steps) > 0 {
			return steps[len(steps)-1]
		}
	}
	return r.shadowDistance
}

// shadowFitFor caps the view frustum at the shadow distance and gathers everything the
// fit algorithms share.
//
// The cap is what keeps a shadow map useful: fitted to the whole frustum, a far plane
// kilometres out would spread every texel across the horizon. The default caps at the
// last of the scene the camera can actually see — how far along the view direction the
// scene bounds reach, never past the camera's own far plane — so the slice covers
// everything that can receive a shadow and nothing beyond it. Override with
// Renderer.SetShadowDistance.
//
// distance is how far the slice should reach, in world units from the eye; zero derives
// it. A cascade split with explicit steps passes its last step, since that IS where its
// shadows stop — left to derive, a shorter auto distance would quietly clamp every step
// past it and collapse those cascades onto the same sliver of frustum.
//
// Covering exactly what is visible matters more than it sounds. fitUniform fits a
// bounding SPHERE around this slice, and a sphere around a frustum wedge reaches well
// past it, so a short cap there is invisible — the slop covers it. A cascade fits a much
// shorter slice with far less slop, so the same short cap shows up as a hard band of
// missing shadow at its far edge. A cap that is right to begin with avoids both.
func (r *Renderer) shadowFitFor(mainView scenes.ViewPacket, sceneCenter glm.Vec3f, sceneRadius float32, distance float32) shadowFit {
	corners := frustumCornersWorld(mainView.ViewProjection())
	eye := mainView.Position

	nearCenter := avgCorners(corners, 0)
	farCenter := avgCorners(corners, 4)
	nearDist := nearCenter.Sub(eye).Length()
	farDist := farCenter.Sub(eye).Length()

	shadowDist := distance
	if shadowDist <= 0 {
		forward := farCenter.Sub(nearCenter).Normalize()
		// How far along the view the scene still reaches. A small floor keeps the slice
		// from collapsing when the camera sits on top of the scene, or has it behind.
		reach := sceneCenter.Sub(eye).Dot(forward) + sceneRadius
		shadowDist = min(max(reach, sceneRadius*0.15), farDist)
	}

	t := float32(1)
	if farDist > nearDist {
		t = glm.Clamp((shadowDist-nearDist)/(farDist-nearDist), 0, 1)
	}

	fit := shadowFit{
		eye:         eye,
		forward:     farCenter.Sub(nearCenter).Normalize(),
		nearDist:    nearDist,
		farDist:     nearDist + t*(farDist-nearDist),
		sceneCenter: sceneCenter,
		sceneRadius: sceneRadius,
	}
	for j := range 4 {
		fit.corners[j] = corners[j]
		fit.corners[j+4] = corners[j].Add(corners[j+4].Sub(corners[j]).Scale(t))
	}
	return fit
}

// fitDirectional points a directional light's shadow camera at the view, using the
// renderer's selected algorithm.
func (r *Renderer) fitDirectional(s *shadowResource, l scenes.LightPacket, fit shadowFit) {
	if c, ok := r.Shadows().(ShadowCascaded); ok {
		r.fitCascaded(s, l, c, fit)
		return
	}
	s.cascades = s.cascades[:0] // a previous frame's slices are not this fit's
	r.fitUniform(s, l, fit)
}

// fitUniform aims an orthographic camera to cover the capped view slice, sized to
// enclose it. When that slice is as large as the whole scene (zoomed out) it falls back
// to the scene sphere, so it is never worse than a whole-scene fit; zoomed in, it packs
// resolution into the near view. The camera is pulled back along -dir across the scene
// so occluders between the light and the slice are still captured, and the center is
// snapped to the shadow texel grid so edges don't crawl as the camera moves.
func (r *Renderer) fitUniform(s *shadowResource, l scenes.LightPacket, fit shadowFit) {
	s.fit = r.fitOrtho(s.orthoCamera(), l, fit, s.width)
	s.ndcBias = s.fit.bias
}

// fitOrtho is fitUniform's body, aimed at a camera the caller owns, returning the depth
// bias its choice of box implies. Cascades need this: each slice is an ordinary uniform
// fit, differing only in which camera it writes and how short a range it covers.
//
// size is the resolution of the map REGION this camera renders into, which for a cascade
// is one square of the atlas rather than the whole texture — the texel grid the fit
// snaps to has to be the one it will actually be sampled through.
func (r *Renderer) fitOrtho(cam Camera, l scenes.LightPacket, fit shadowFit, size uint32) orthoFit {
	f, ok := cam.(interface {
		SetPosition(glm.Vec3f)
		SetTarget(glm.Vec3f)
		SetUp(glm.Vec3f)
		SetFrustum(l, r, b, t float32)
		SetClip(near, far float32)
	})
	if !ok {
		return orthoFit{}
	}
	d := l.Direction.Normalize()

	// Bounding sphere of the capped slice; fall back to the scene sphere when the fit
	// isn't tighter (e.g. zoomed out), so we never do worse than whole-scene.
	center, radius := boundingSphere(fit.corners)
	if radius >= fit.sceneRadius {
		center, radius = fit.sceneCenter, fit.sceneRadius
	}

	// Quantize the radius so the texel size only changes in discrete steps as the camera
	// zooms. A continuously-resizing box would keep moving the texel grid under the
	// geometry, which is what makes edges crawl — snapping the center only helps while
	// the texel size holds still.
	//
	// The step has to be RELATIVE to the radius, not an absolute fraction of the scene.
	// An absolute step is a fixed number of world units, so it is invisible on a slice
	// far larger than one step and ruinous on a slice smaller than one: in a scene a few
	// hundred units across it rounded a three-unit near slice up to fourteen, throwing
	// away four fifths of the resolution exactly where the fit was trying to concentrate
	// it. Rounding up on a geometric grid costs the same proportion at every scale, which
	// is what makes it safe to fit a small slice at all. Quarter-octave steps waste at
	// most a fifth of the resolution, against the factor of two a whole-octave grid costs
	// on a large one.
	if radius > 0 {
		const stepsPerOctave = 4
		octave := math.Ceil(math.Log2(float64(radius))*stepsPerOctave) / stepsPerOctave
		radius = float32(math.Exp2(octave))
	}
	right, up := lightBasis(d)

	// Snap the center to the shadow texel grid in the light's right/up plane. This fit
	// is always square, so either axis names the same texel.
	texel := 2 * radius / float32(size)
	if texel > 0 {
		cx := snap(center.Dot(right), texel)
		cy := snap(center.Dot(up), texel)
		cz := center.Dot(d)
		center = right.Scale(cx).Add(up.Scale(cy)).Add(d.Scale(cz))
	}

	const near, farScale = float32(0.01), float32(4)
	far := fit.sceneRadius * farScale
	f.SetUp(up)
	f.SetPosition(center.Sub(d.Scale(fit.sceneRadius * 2))) // back up across the scene
	f.SetTarget(center)
	f.SetFrustum(-radius, radius, -radius, radius)
	f.SetClip(near, far) // depth spans the whole scene toward the light

	// The renderer decides where the camera goes; the caller owns the derived bias.
	return orthoBias(radius, far-near, size, l.ShadowBias)
}

// fitCascaded fits one orthographic camera per slice of the view. Each slice is the
// same frustum capped to a shorter range, so every cascade is an ordinary uniform fit —
// texel snapping and all — over a range short enough for its texels to matter.
func (r *Renderer) fitCascaded(s *shadowResource, l scenes.LightPacket, c ShadowCascaded, fit shadowFit) {
	count := c.levels()
	s.ensureCascades(count)

	var splits [MaxShadowCascades]float32
	if explicit := c.steps(); explicit != nil {
		copy(splits[:count], explicit)
	} else {
		splitNear := r.shadowNear
		if splitNear <= 0 {
			splitNear = fit.farDist * defaultShadowNear
		}
		cascadeSplits(max(splitNear, fit.nearDist), fit.farDist, splits[:count])
	}

	// The first cascade is fitted from the camera's real near plane, whatever the split
	// started from, so geometry in between is still covered.
	near := fit.nearDist
	for i := range count {
		// Explicit steps are the caller's, so they are guarded rather than trusted: a
		// step that does not advance would give a slice no depth to fit.
		far := max(splits[i], near*(1+1e-3))
		lvl := &s.cascades[i]
		lvl.far = far
		// The slice's own frustum: the same corner rays, cut at this cascade's near and
		// far rather than the whole range's.
		sub := fit
		sub.nearDist, sub.farDist = near, far
		sub.corners = sliceCorners(fit, near, far)
		lvl.fit = r.fitOrtho(lvl.cam, l, sub, s.size())
		near = far
	}
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

// --------------------------------------------------------------------------------------
// Pipelines
//
// The renderer owns every pipeline. Draw pipelines are built lazily per material raster
// state, and all of them are rebuilt when the target format changes.
// --------------------------------------------------------------------------------------

// buildPipelines (re)creates the cull pipeline and every draw pipeline from the
// registered material shaders (called on target/format change).
func (r *Renderer) buildPipelines() {
	if r.pipelinesReady {
		r.backend.DestroyPipeline(r.cullPipeline)
		r.backend.DestroyPipeline(r.skinPipeline)
		r.backend.DestroyPipeline(r.particleUpdatePipeline)
		r.backend.DestroyPipeline(r.particleSortKeysPipeline)
		r.backend.DestroyPipeline(r.particleSortStepPipeline)
		r.backend.DestroyPipeline(r.fogInjectPipeline)
		r.backend.DestroyPipeline(r.fogIntegratePipeline)
		r.backend.DestroyPipeline(r.shadowPipeline)
		for _, p := range r.prepassPipelines {
			r.backend.DestroyPipeline(p)
		}
		for _, p := range r.drawPipelines {
			r.backend.DestroyPipeline(p)
		}
	}
	r.cullPipeline = r.backend.CreateComputePipeline(gpu.ComputePipelineDescriptor{Shader: shaders.ForBackend(r.backend, shaders.SceneCull), Entry: "main", Label: "scene-cull"})
	r.skinPipeline = r.backend.CreateComputePipeline(gpu.ComputePipelineDescriptor{Shader: shaders.ForBackend(r.backend, shaders.SceneSkin), Entry: "main", Label: "scene-skin"})
	r.particleUpdatePipeline = r.backend.CreateComputePipeline(gpu.ComputePipelineDescriptor{Shader: shaders.ForBackend(r.backend, shaders.ParticleUpdate), Entry: "main", Label: "particle-update"})
	r.particleSortKeysPipeline = r.backend.CreateComputePipeline(gpu.ComputePipelineDescriptor{Shader: shaders.ForBackend(r.backend, shaders.ParticleSortKeys), Entry: "main", Label: "particle-sort-keys"})
	r.particleSortStepPipeline = r.backend.CreateComputePipeline(gpu.ComputePipelineDescriptor{Shader: shaders.ForBackend(r.backend, shaders.ParticleSortStep), Entry: "main", Label: "particle-sort-step"})
	r.fogInjectPipeline = r.backend.CreateComputePipeline(gpu.ComputePipelineDescriptor{Shader: shaders.ForBackend(r.backend, shaders.FogInject), Entry: "main", Label: "fog-inject"})
	r.fogIntegratePipeline = r.backend.CreateComputePipeline(gpu.ComputePipelineDescriptor{Shader: shaders.ForBackend(r.backend, shaders.FogIntegrate), Entry: "main", Label: "fog-integrate"})
	// Depth-only passes: position-only vertex-pull, no colour attachment, writes depth.
	//
	// No fragment shader at all: a stage that outputs nothing is not free — it still
	// runs per fragment — and leaving it out is what lets the driver take its
	// depth-only path.
	depthOnly := func(cull gpu.CullMode, label string) gpu.Pipeline {
		return r.backend.CreateGraphicsPipeline(gpu.PipelineDescriptor{
			VertexShader: shaders.ForBackend(r.backend, shaders.SceneShadowVert),
			Topology:     gpu.TopologyTriangles, DepthFormat: gpu.FormatDepth32F,
			DepthTest: true, DepthWrite: true, DepthCompare: gpu.CompareGreater,
			CullMode: cull, FrontFaceCW: true, Label: label,
		})
	}
	// Culling is off for the shadow pass: it only decides which of a surface's two
	// faces writes the depth they share, and thin geometry must still occlude from the
	// light's view.
	r.shadowPipeline = depthOnly(gpu.CullNone, "scene-shadow")

	// The depth prepass is the same pass pointed at the view camera, but one pipeline
	// per cull mode: a prepass must cull exactly as the shading pass will. Sharing a
	// single double-sided pipeline writes depth for faces the shading pass discards,
	// and a surface the viewer was never meant to see then occludes everything behind
	// it — a far wall hiding the room, terrain hiding what is under it.
	for i := range r.prepassPipelines {
		r.prepassPipelines[i] = depthOnly(gpu.CullMode(i), "scene-depth-prepass")
	}

	for i, k := range r.drawPipelineKeys {
		r.drawPipelines[i] = r.buildDrawPipe(k)
	}
	r.buildToneMapPass()
	r.pipelinesReady = true
	if r.overlay != nil {
		r.overlay.destroy()
		r.overlay = newOverlay(r.backend, r.TextureStore, r.scale, r.color, gpu.FormatDepth32F)
	}
}

// materialPipeline is a material's full pipeline identity: shaders + raster state.
type materialPipeline struct {
	shaderHash       uint64 // cached (vertex,fragment) identity — the dedup key
	vertex, fragment []byte // kept only to build the pipeline
	cull             materials.CullMode
	blend            materials.BlendMode
}

// buildDrawPipe creates a graphics pipeline for a material pipeline key against the
// current color/depth formats. The vertex-pull stage is shared unless the material
// supplies its own vertex program. Every key targets the HDR scene image, with its
// material's blend mode: materials output linear, unclamped light, and blending happens
// in linear light where it is physically meaningful.
func (r *Renderer) buildDrawPipe(k materialPipeline) gpu.Pipeline {
	vert := k.vertex
	if vert == nil {
		vert = shaders.SceneDraw
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
		Topology: gpu.TopologyTriangles, ColorFormats: []gpu.Format{r.sceneFormat()},
		// GreaterEqual, not Greater: with a depth prepass the shading draw meets depth it
		// wrote itself, and Greater would reject every fragment. Without one the only
		// difference is which of two exactly-coplanar surfaces wins, where nothing was
		// well defined to begin with.
		DepthFormat: gpu.FormatDepth32F, DepthTest: true, DepthWrite: depthWrite, DepthCompare: gpu.CompareGreaterEqual,
		// The renderer flips clip-space Y (Vulkan NDC is Y-down), which reverses
		// triangle winding, so front faces are clockwise on screen.
		CullMode: gpu.CullMode(k.cull), FrontFaceCW: true, Blend: blend,
	})
}

// buildToneMapPass (re)creates the pass that maps the HDR frame into the target, which
// depends on the target's format. Without HDR there is none.
func (r *Renderer) buildToneMapPass() {
	if r.toneMapPass != nil {
		r.toneMapPass.Release()
		r.toneMapPass = nil
	}
	if r.hdr {
		r.toneMapPass = postprocess.NewFullscreenPass(r.backend, postprocess.FullscreenPassDescriptor{
			Fragment: shaders.ToneMap,
			Format:   r.color,
			Label:    "tone-map",
		})
	}
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

// pipelineUnresolved marks a cell of a pool's pipeline table that has not been built
// yet. Zero is a perfectly good pipeline index, so it cannot double as "empty".
const pipelineUnresolved uint32 = 0xFFFFFFFF

// poolPipelines is every draw pipeline the instances of one material pool can select
// between. A pool is keyed by a Shader that never changes for its lifetime, so the
// only things that vary within it are the per-instance cull and blend modes. Three
// values each, so the entire space is a fixed table indexed directly — no hashing, no
// scan, no map — filled lazily because most pools use one or two of the cells.
type poolPipelines struct {
	table [3][3]uint32 // [cull][blend]
}

func newPoolPipelines() poolPipelines {
	var pp poolPipelines
	for c := range pp.table {
		for b := range pp.table[c] {
			pp.table[c][b] = pipelineUnresolved
		}
	}
	return pp
}

// pipelineForPool resolves the draw pipeline for one material of a pool, given that
// material's rasterization state.
//
// The shaders come from the pool rather than the material because the pool is keyed by
// those very shaders — a material cannot disagree with it, and so cannot ask for a
// pipeline built out of SPIR-V its pool does not have.
func (r *Renderer) pipelineForPool(p *materials.Pool, cull materials.CullMode, blend materials.BlendMode) uint32 {
	for uint32(len(r.pools)) <= p.Index() {
		r.pools = append(r.pools, newPoolPipelines())
	}
	pp := &r.pools[p.Index()]

	if id := pp.table[cull][blend]; id != pipelineUnresolved {
		return id
	}
	sh := p.Shader()
	id := r.pipelineFor(materialPipeline{
		shaderHash: p.Hash(), vertex: sh.Vertex, fragment: sh.Fragment,
		cull: cull, blend: blend,
	})
	pp.table[cull][blend] = id
	return id
}

// sameKey compares two pipeline keys.
func sameKey(a, b materialPipeline) bool {
	return a.shaderHash == b.shaderHash && a.cull == b.cull && a.blend == b.blend
}

// debugViewActive reports whether this frame draws a debug view instead of shading.
func (r *Renderer) debugViewActive() bool {
	return r.debugView != DebugOff && int(r.debugView) < len(debugViewNames)
}

// debugSurfaceView reports whether the active view runs over the ordinary scene vertex
// stage (reading its varyings) rather than the id pass's own one.
func (r *Renderer) debugSurfaceView() bool {
	return r.debugView == DebugNormal || r.debugView == DebugDepth || r.debugView == DebugPosition
}

// debugPipelineFor builds (once per view) the pipeline that draws it.
//
// CullNone, not CullBack: a drawable's own material may be double-sided or
// front-culled, and one shared pipeline has no per-drawable way to know which —
// hardcoding CullBack drops every backface of any double-sided object (foliage,
// glass), which shows as missing geometry. Nothing here depends on winding.
func (r *Renderer) debugPipelineFor(v DebugView) gpu.Pipeline {
	if p := r.debugPipelines[v]; p.H != 0 {
		return p
	}
	vert := shaders.SceneDebugIDVert
	if r.debugSurfaceView() {
		vert = shaders.SceneDraw
	}
	p := r.backend.CreateGraphicsPipeline(gpu.PipelineDescriptor{
		VertexShader:   shaders.ForBackend(r.backend, vert),
		FragmentShader: shaders.ForBackend(r.backend, debugFragment(v)),
		Topology:       gpu.TopologyTriangles, ColorFormats: []gpu.Format{r.color},
		DepthFormat: gpu.FormatDepth32F, DepthTest: true, DepthWrite: true, DepthCompare: gpu.CompareGreater,
		CullMode: gpu.CullNone, FrontFaceCW: true, Label: "debug-" + v.String(),
	})
	r.debugPipelines[v] = p
	return p
}

// --------------------------------------------------------------------------------------
// Overlay and console
// --------------------------------------------------------------------------------------

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

// registerBuiltins exposes the renderer's own switches on a freshly created console,
// so a console is useful the moment it is enabled rather than only after the
// application has bound things by hand. Every one of these was previously reachable
// only by editing code and rebuilding.
//
// Names are dotted and grouped by subject (shadow.*, stats.*) so `list shadow.` filters
// usefully and tab completion narrows in the same way.
func (r *Renderer) registerBuiltins(c *console.Console) {
	console.BindFunc(c, "shadows", r.ShadowsEnabled,
		func(v bool) error { r.EnableShadows(v); return nil },
		"render shadow maps")

	console.BindFunc(c, "shadow.distance", r.ShadowDistance,
		func(v float32) error { r.SetShadowDistance(v); return nil },
		"directional shadow fit distance, world units (0 = auto)")

	c.Register("shadow.filter",
		"kernel directional shadow lookups use: "+strings.Join(ShadowFilterNames(), "/"),
		func() string { return r.ShadowFilter().String() },
		func(v string) error {
			filter, ok := ParseShadowFilter(v)
			if !ok {
				return fmt.Errorf("unknown filter %q; want one of %s", v, strings.Join(ShadowFilterNames(), ", "))
			}
			r.SetShadowFilter(filter)
			return nil
		})

	console.BindFunc(c, "shadow.near", r.ShadowNear,
		func(v float32) error { r.SetShadowNear(v); return nil },
		"cascaded: distance the split starts from, world units (0 = auto)")

	console.BindFunc(c, "shadow.cascades", func() uint32 { return uint32(cascadeSettings(r).Levels) },
		func(v uint32) error {
			if v == 0 || v > MaxShadowCascades {
				return fmt.Errorf("cascades must be 1..%d", MaxShadowCascades)
			}
			cs := cascadeSettings(r)
			cs.Levels = int(v)
			r.SetShadows(cs)
			return nil
		},
		"cascaded: how many slices the view is split into")

	// The boundaries as a comma list, which is what tuning a scene actually comes down
	// to: "shadow.steps 8,25,80" is the whole of it. Empty derives them.
	c.Register("shadow.steps",
		"cascaded: boundary distances, comma separated, innermost first, or \"auto\"",
		func() string {
			cs := cascadeSettings(r)
			if cs.AutoSteps || len(cs.Steps) == 0 {
				return "auto"
			}
			parts := make([]string, len(cs.Steps))
			for i, v := range cs.Steps {
				parts[i] = strconv.FormatFloat(float64(v), 'g', -1, 32)
			}
			return strings.Join(parts, ",")
		},
		func(v string) error {
			cs := cascadeSettings(r)
			v = strings.TrimSpace(v)
			if v == "" || v == "auto" {
				cs.Steps, cs.AutoSteps = nil, true
				r.SetShadows(cs)
				return nil
			}
			fields := strings.Split(v, ",")
			if len(fields) > MaxShadowCascades {
				return fmt.Errorf("at most %d steps", MaxShadowCascades)
			}
			steps := make([]float32, 0, len(fields))
			prev := float32(0)
			for _, f := range fields {
				d, err := strconv.ParseFloat(strings.TrimSpace(f), 32)
				if err != nil {
					return fmt.Errorf("step %q is not a distance", f)
				}
				if float32(d) <= prev {
					return fmt.Errorf("steps must increase; %g does not follow %g", d, prev)
				}
				prev = float32(d)
				steps = append(steps, float32(d))
			}
			cs.Steps, cs.AutoSteps = steps, false
			r.SetShadows(cs)
			return nil
		})

	// The fit is a choice between two named shapes rather than a number, so it goes
	// through Register — "set shadow.algorithm cascaded" reads better than a magic value,
	// and the error lists what is valid. Switching keeps whatever cascade settings were
	// already there, so flipping back and forth does not discard a tuned split.
	c.Register("shadow.algorithm",
		"how directional shadow cameras are fitted: uniform/cascaded",
		func() string {
			if _, ok := r.Shadows().(ShadowCascaded); ok {
				return "cascaded"
			}
			return "uniform"
		},
		func(v string) error {
			switch v {
			case "uniform":
				r.SetShadows(ShadowUniform{})
			case "cascaded":
				r.SetShadows(cascadeSettings(r))
			default:
				return fmt.Errorf("unknown algorithm %q; want one of uniform, cascaded", v)
			}
			return nil
		})

	console.BindFunc(c, "depthprepass", r.DepthPrepassEnabled,
		func(v bool) error { r.EnableDepthPrepass(v); return nil },
		"fill depth before shading; helps desktop GPUs, costs Apple GPUs (they already shade each pixel once)")

	console.BindFunc(c, "hdr", r.HDREnabled,
		func(v bool) error {
			r.EnableHDR(v)
			return nil
		},
		"render in HDR: offscreen scene image, post-processing, tone mapping")

	console.BindFunc(c, "exposure", r.Exposure,
		func(v float32) error {
			r.SetExposure(v)
			return nil
		},
		"HDR light scale before tone mapping, in stops")

	console.BindFunc(c, "stats", r.StatsVisible,
		func(v bool) error { r.ShowFPS(v); return nil },
		"show the FPS / CPU / GPU HUD")

	bindColor(c, "stats.color", r.FontColor, r.SetFontColor,
		"HUD text colour, \"r g b a\" in [0,1]")

	bindColor(c, "clear.color", r.ClearColor, r.SetClearColor,
		"background colour, \"r g b a\" in [0,1]")

	// The debug view is an enum, so it goes through Register with its own names
	// rather than Bind — "set debug normal" reads better than a magic number, and
	// the error lists what is valid. Every view is a geometry pass over the scene, so
	// none of them depends on any other setting being on.
	c.Register("debug", "show one debug view instead of the shaded frame: "+
		strings.Join(DebugViewNames(), "/"),
		func() string { return r.DebugView().String() },
		func(v string) error {
			view, ok := ParseDebugView(v)
			if !ok {
				return fmt.Errorf("unknown view %q; want one of %s", v, strings.Join(DebugViewNames(), ", "))
			}
			r.SetDebugView(view)
			return nil
		})

	// Read-only: no setter, so the console reports them rather than pretending they
	// can be assigned. Resizing is driven by the window, not by a variable.
	c.Register("size", "framebuffer size in pixels",
		func() string { w, h := r.Size(); return fmt.Sprintf("%dx%d", w, h) }, nil)
	c.Register("aspect", "framebuffer aspect ratio",
		func() string { return fmt.Sprintf("%.4f", r.Aspect()) }, nil)
}

// registerCommands adds the console commands that act on the renderer. Unlike a
// variable, these do something once rather than holding a value.
func (r *Renderer) registerCommands(c *console.Console) {
	c.Command("screenshot", "save a PNG of this frame; defaults to a timestamped name",
		func(args []string) error {
			path := ""
			if len(args) > 0 {
				path = strings.Join(args, " ")
			}
			// The capture happens at the end of this very frame, so the result is
			// reported from the callback rather than returned.
			r.Screenshot(path, func(p string, err error) {
				if err != nil {
					c.Printf("screenshot failed: %v", err)
					return
				}
				c.Printf("wrote %s", p)
			})
			return nil
		})
}

// --------------------------------------------------------------------------------------
// Teardown
// --------------------------------------------------------------------------------------

// ReleaseSource drops the GPU state cached for a producer. Call it when a Scene is
// destroyed, passing Scene.ID: scene teardown does not reach into a renderer to do
// this, so nothing else will.
//
// Releasing a source that is drawn again is safe, but not free, and not lossless: the
// caches rebuild from the next packet, while each particle system restarts empty — its
// simulation lived only in the released buffers — and LOD hysteresis starts over.
//
// Renderer.Destroy releases every remaining source, so a program that tears the renderer
// down at exit need not track this.
func (r *Renderer) ReleaseSource(id scenes.SourceID) {
	st, ok := r.sources[id]
	if !ok {
		return
	}
	r.releaseState(st)
	delete(r.sources, id)
}

// releaseState frees everything one source's state holds.
func (r *Renderer) releaseState(st *renderState) {
	buffers := []gpu.Buffer{st.worldBuf, st.drawableBuf, st.lodTableBuf, st.jointBuf, st.prevLevelBuf,
		st.mainCull.indirectBuf, st.mainCull.visibleBuf}
	for _, cull := range st.shadowCulls {
		buffers = append(buffers, cull.indirectBuf, cull.visibleBuf)
	}
	for _, buf := range buffers {
		if buf.IsValid() {
			r.backend.Free(buf)
		}
	}

	st.lights.Destroy()
	for _, sh := range st.shadows {
		sh.destroy()
	}
	for _, ps := range st.particles {
		ps.destroy(r.backend)
	}
}

// Destroy releases the renderer's GPU resources and the backend it owns.
func (r *Renderer) Destroy() {
	for id := range r.sources {
		r.ReleaseSource(id)
	}
	if r.overlay != nil {
		r.overlay.destroy()
	}
	r.profiler.disableGPUProfiling(r.backend)
	if r.pipelinesReady {
		r.backend.DestroyPipeline(r.cullPipeline)
		r.backend.DestroyPipeline(r.skinPipeline)
		r.backend.DestroyPipeline(r.particleUpdatePipeline)
		r.backend.DestroyPipeline(r.particleSortKeysPipeline)
		r.backend.DestroyPipeline(r.particleSortStepPipeline)
		r.backend.DestroyPipeline(r.fogInjectPipeline)
		r.backend.DestroyPipeline(r.fogIntegratePipeline)
		r.backend.DestroyPipeline(r.shadowPipeline)
		for _, p := range r.prepassPipelines {
			r.backend.DestroyPipeline(p)
		}
		for _, p := range r.drawPipelines {
			r.backend.DestroyPipeline(p)
		}
	}
	if r.toneMapPass != nil {
		r.toneMapPass.Release()
		r.toneMapPass = nil
	}
	for _, p := range r.debugPipelines {
		if p.H != 0 {
			r.backend.DestroyPipeline(p)
		}
	}
	for _, step := range r.postProcessing {
		step.Release()
	}
	r.postProcessing = nil
	for stage, steps := range r.frameSteps {
		for _, step := range steps {
			step.Release()
		}
		r.frameSteps[stage] = nil
	}
	r.releaseFogVolumes()
	if r.fogBackgroundPass != nil {
		r.fogBackgroundPass.Release()
	}
	for _, t := range []gpu.Texture{r.depth, r.sceneColor, r.postColor} {
		if t.IsValid() {
			r.backend.DestroyTexture(t)
		}
	}
	for _, sampler := range []gpu.Sampler{r.shadowSampler, r.linearSampler} {
		if sampler.H != 0 {
			r.backend.DestroySampler(sampler)
		}
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
