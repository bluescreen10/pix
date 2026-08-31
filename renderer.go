package pix

import (
	"fmt"
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

	cullPipeline gpu.Pipeline
	// Draw pipelines, one per distinct material pipeline key (shaders + cull + blend).
	// drawPipelineKeys is parallel so pipelineFor can dedup and buildPipelines can rebuild
	// them all when the target format changes.
	drawPipelines    []gpu.Pipeline
	drawPipelineKeys []materialPipeline
	pipelinesReady   bool

	// Stats / debug HUD.
	stats     *RendererStats
	fontColor glm.RGBA32F
	showFPS   bool
	overlay   *overlay
	gpuPool   gpu.QueryPool
	gpuValid  bool
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
		backend:    backend,
		clear:      glm.RGBA32F{0, 0, 0, 1},      //TODO: extract as constant
		fontColor:  glm.RGBA32F{1, 0.9, 0.35, 1}, //TODO: extract as constant
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

// configure sets the size/format, (re)creates the depth buffer and the pipelines.
func (r *Renderer) configure(w, h uint32, format gpu.Format) {
	r.width, r.height, r.color = w, h, format
	if r.depth.Valid() {
		r.backend.DestroyTexture(r.depth)
	}
	r.depth = r.backend.CreateTexture(gpu.TextureDescriptor{Kind: gpu.Texture2D, Width: w, Height: h,
		Format: gpu.FormatDepth32F, Usage: gpu.TextureDepth})
	r.buildPipelines()
}

// buildPip elines (re)creates the cull pipeline and every draw pipeline from the
// registered material shaders (called on target/format change).
func (r *Renderer) buildPipelines() {
	if r.pipelinesReady {
		r.backend.DestroyPipeline(r.cullPipeline)
		for _, p := range r.drawPipelines {
			r.backend.DestroyPipeline(p)
		}
	}
	r.cullPipeline = r.backend.CreateComputePipeline(gpu.ComputePipelineDescriptor{Shader: shaders.SceneCull, Entry: "main", Label: "scene-cull"})
	for i, k := range r.drawPipelineKeys {
		r.drawPipelines[i] = r.buildDrawPipe(k)
	}
	r.pipelinesReady = true
	if r.overlay != nil {
		r.overlay.destroy()
		r.overlay = newOverlay(r.backend, r.color, gpu.FormatDepth32F)
	}
}

// materialPipeline is a material's full pipeline identity: shaders + raster state.
type materialPipeline struct {
	shaderHash       uint32 // cached (vertex,fragment) identity — the dedup key
	vertex, fragment []byte // kept only to build the pipeline
	cull             CullMode
	blend            BlendMode
}

// buildDrawPipe creates a graphics pipeline for a material pipeline key against the
// current color/depth formats. The vertex-pull stage is shared unless the material
// supplies its own vertex program.
func (r *Renderer) buildDrawPipe(k materialPipeline) gpu.Pipeline {
	vert := k.vertex
	if vert == nil {
		vert = shaders.SceneDraw
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
func (r *Renderer) pipelineForMaterial(m Material) uint32 {
	return r.pipelineFor(materialPipeline{
		shaderHash: m.shaderHash(), vertex: m.Vertex(), fragment: m.Fragment(), cull: m.Cull(), blend: m.Blend(),
	})
}

func sameKey(a, b materialPipeline) bool {
	return a.shaderHash == b.shaderHash && a.cull == b.cull && a.blend == b.blend
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
func (r *Renderer) Render(scene *Scene, cam *Camera) {
	//TODO: r.swapchain.H == 0 is a bit of a smell something like r.swapchain.Valid()
	// would be better
	if !r.hasTarget && r.swapchain.H == 0 {
		panic("renderer has no target")
	}
	r.stats.StartFrame()
	prepStart := time.Now()
	r.syncScene(scene)
	vp := cam.ViewProj()
	planes := FrustumPlanes(vp)
	drawVP := flipClipY(vp)
	eye := cam.Position
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
	cmd := r.backend.Begin()
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

// encode records the cull + lit draw (and, when enabled, GPU timestamps + the HUD).
func (r *Renderer) encode(cmd gpu.CommandBuffer, target gpu.Texture, scene *Scene, planes [6][4]float32, drawVP glm.Mat4f, eye glm.Vec3f) {
	if r.showFPS {
		cmd.ResetTimestamps(r.gpuPool, 2)
		cmd.WriteTimestamp(r.gpuPool, 0, gpu.StageNone)
	}
	r.recordCull(cmd, scene.drawList, planes)
	cmd.BeginRenderPass(gpu.RenderTargets{
		Color: []gpu.ColorAttachment{{Texture: target, Load: gpu.LoadClear, Clear: r.clear}},
		Depth: &gpu.DepthAttachment{Texture: r.depth, Load: gpu.LoadClear, Clear: 1.0},
	})
	cmd.Viewport(0, 0, float32(r.width), float32(r.height), 0, 1)
	cmd.Scissor(0, 0, int32(r.width), int32(r.height))
	r.recordDraw(cmd, scene.drawList, drawVP, eye, scene.lights.Addr())
	if r.showFPS && r.overlay != nil {
		r.overlay.draw(cmd, float32(r.width), float32(r.height))
	}
	cmd.EndRenderPass()
	if r.showFPS {
		cmd.WriteTimestamp(r.gpuPool, 1, gpu.StageColorOutput)
	}
}

// syncScene uploads the renderer's shared tables + the scene's per-scene GPU state.
// It resolves each mesh's draw pipeline from its material (shaders + raster) every
// frame and rebuilds the draw list when the mesh set OR any pipeline assignment
// changed (so a material raster/blend change re-batches without an explicit dirty).
func (r *Renderer) syncScene(scene *Scene) {
	// Upload-once device resources (geometry streams + descriptors) are staged
	// through a single uploader: each subsystem records copies, then flush submits
	// and waits before the frame's draws read them.
	// The renderer owns geometry (shared) and pipelines; the scene owns transforms,
	// its draw list, and lights. Both stage into one uploader, flushed once here.
	// Material records live in host-coherent mapped memory — already visible, no upload.
	up := newUploader(r.backend)
	r.geometries.Sync(up)
	scene.Sync(up)
	up.flush()

	dl := scene.drawList
	dl.pipeBuf = dl.pipeBuf[:0]
	for i := range scene.meshes {
		//TODO: maybe the material should have a []pipelineKey (or pipelineID) stored
		dl.pipeBuf = append(dl.pipeBuf, r.pipelineForMaterial(scene.meshes[i].material))
	}
	if scene.drawableDirty || !slices.Equal(dl.pipeBuf, dl.batchedPipelines) {
		drawables, materials := scene.collectDrawables()
		dl.rebuild(drawables, dl.pipeBuf, materials, r.geometries)
		scene.drawableDirty = false
	}
}

func (r *Renderer) recordCull(cmd gpu.CommandBuffer, dl *drawList, planes [6][4]float32) {
	if dl.batchCount() == 0 {
		return
	}
	writeAt(dl.indirectBuf, 0, toBytes(dl.template))
	*(*cullRoot)(dl.cullRootBuf.Ptr) = cullRoot{
		drawables: dl.drawableBuf.Addr, models: dl.worldBuf.Addr, indirect: dl.indirectBuf.Addr,
		regions: dl.regionBuf.Addr, visible: dl.visibleBuf.Addr, count: dl.numInst, planes: planes,
	}
	cmd.SetPipeline(r.cullPipeline)
	cmd.Root(dl.cullRootBuf.Addr)
	cmd.Dispatch((dl.numInst+63)/64, 1, 1)
	cmd.Barrier(gpu.StageCompute, gpu.StageIndirect|gpu.StageVertex, 0)
}

// recordDraw issues one multi-draw-indirect call per pipeline run: all of a
// material type's per-geometry commands go in a single DrawIndexedIndirect. Each
// command's firstInstance is its region base, so gl_InstanceIndex indexes the
// compacted visible buffer directly — no per-command push constant.
func (r *Renderer) recordDraw(cmd gpu.CommandBuffer, dl *drawList, viewProj glm.Mat4f, eye glm.Vec3f, lightsAddr uint64) {
	if len(dl.runs) == 0 {
		return
	}
	roots := unsafe.Slice((*drawRoot)(dl.drawRootBuf.Ptr), len(dl.runs))
	idx := r.geometries.IndexBuffer()
	for ri := range dl.runs {
		run := &dl.runs[ri]
		roots[ri] = drawRoot{
			viewProj:  viewProj,
			pos:       r.geometries.PositionsAddr(),
			attr:      r.geometries.AttributesAddr(),
			descs:     r.geometries.DescriptorsAddr(),
			models:    dl.worldBuf.Addr,
			drawables: dl.drawableBuf.Addr,
			visible:   dl.visibleBuf.Addr,
			materials: run.mat.materialsAddr(),
			lights:    lightsAddr,
			eye:       glm.Vec4f{eye[0], eye[1], eye[2], 1},
		}
		cmd.SetPipeline(r.drawPipelines[run.pipeline])
		cmd.Root(dl.drawRootBuf.Addr + uint64(ri)*drawRootSize)
		cmd.DrawIndexedIndirect(idx, dl.indirectBuf, uint64(run.firstBatch)*uint64(indirectSize), run.count, indirectSize)
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
		for _, p := range r.drawPipelines {
			r.backend.DestroyPipeline(p)
		}
	}
	if r.depth.Valid() {
		r.backend.DestroyTexture(r.depth)
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
	r.backend.Destroy()
}

//TODO: Add a resize method that would adjust the SwapChain size
