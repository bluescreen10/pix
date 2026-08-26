package pix

import (
	"fmt"
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
	backend gpu.Backend
	w, h    uint32
	color   gpu.Format
	clear   [4]float32
	depth   gpu.Texture

	// Presentation target: a swapchain (windowed) or a render-target texture (headless).
	sc         gpu.Swapchain
	target     gpu.Texture
	hasTarget  bool
	ownsTarget bool // renderer created the target (via NewOffscreenRenderer)
	readback   gpu.Buffer
	pixels     []byte

	// Renderer-owned shared resources + GPU-driven pipelines.
	geo            *geometrySystem
	mats           *materialSystem
	texs           *textureSystem
	cullPipe       gpu.Pipeline
	drawPipe       gpu.Pipeline
	pipelinesReady bool

	// Stats / debug HUD.
	Stats     *RendererStats
	FontColor [4]float32
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
	b := gpu.Instance(name)
	if err := b.Init(); err != nil {
		return nil, fmt.Errorf("render: init backend: %w", err)
	}
	r := &Renderer{backend: b, clear: [4]float32{0, 0, 0, 1}}
	r.geo = newGeometrySystem(b)
	r.mats = newMaterialSystem(b)
	r.texs = newTextureSystem(b)
	r.Stats = NewRendererStats(60)
	r.FontColor = [4]float32{1, 0.9, 0.35, 1}

	switch {
	case cfg.Window != nil:
		r.attachWindow(cfg.Window, cfg.Width, cfg.Height) // platform-specific
	case cfg.Width > 0 && cfg.Height > 0:
		r.attachOffscreen(cfg.Width, cfg.Height)
	}
	return r, nil
}

// attachOffscreen configures an internally-owned RGBA8 render target of w×h.
func (r *Renderer) attachOffscreen(w, h uint32) {
	tex := r.backend.CreateTexture(gpu.TextureDescriptor{Kind: gpu.Texture2D, Width: w, Height: h,
		Format: gpu.FormatRGBA8Unorm, Usage: gpu.TextureRenderTarget | gpu.TextureTransfer})
	r.ownsTarget = true
	r.SetRenderTarget(tex, w, h, gpu.FormatRGBA8Unorm)
}

// NewOffscreenRenderer is a convenience for a headless renderer with an
// internally-owned RGBA8 target of the given size (read it with Pixels/Capture).
func NewOffscreenRenderer(w, h uint32) (*Renderer, error) {
	return NewRenderer(&RendererConfig{Width: w, Height: h})
}

// SetRenderTarget renders into tex (headless). The renderer (re)creates its depth
// buffer and pipelines to match. tex must have render-target usage (plus transfer
// usage if you intend to Capture it).
func (r *Renderer) SetRenderTarget(tex gpu.Texture, w, h uint32, format gpu.Format) {
	r.target = tex
	r.hasTarget = true
	r.sc = gpu.Swapchain{}
	r.configure(w, h, format)
}

// configure sets the size/format, (re)creates the depth buffer and the pipelines.
func (r *Renderer) configure(w, h uint32, format gpu.Format) {
	r.w, r.h, r.color = w, h, format
	if r.depth.Valid() {
		r.backend.DestroyTexture(r.depth)
	}
	r.depth = r.backend.CreateTexture(gpu.TextureDescriptor{Kind: gpu.Texture2D, Width: w, Height: h,
		Format: gpu.FormatDepth32F, Usage: gpu.TextureDepth})
	r.buildPipelines()
}

func (r *Renderer) buildPipelines() {
	if r.pipelinesReady {
		r.backend.DestroyPipeline(r.cullPipe)
		r.backend.DestroyPipeline(r.drawPipe)
	}
	r.cullPipe = r.backend.CreateComputePipeline(gpu.ComputePipelineDescriptor{Shader: shaders.SceneCull, Entry: "main", Label: "scene-cull"})
	r.drawPipe = r.backend.CreateGraphicsPipeline(gpu.PipelineDescriptor{
		VertexShader: shaders.SceneDraw, FragmentShader: shaders.SceneLit,
		Topology: gpu.TopologyTriangles, ColorFormats: []gpu.Format{r.color},
		DepthFormat: gpu.FormatDepth32F, DepthTest: true, DepthWrite: true, DepthCompare: gpu.CompareLess,
		CullMode: gpu.CullNone,
	})
	r.pipelinesReady = true
	if r.overlay != nil {
		r.overlay.destroy()
		r.overlay = newOverlay(r.backend, r.color, gpu.FormatDepth32F)
	}
}

// NewGeometry uploads mesh data and returns a ref-counted geometry handle.
func (r *Renderer) NewGeometry(data Data) Geometry { return r.geo.newHandle(data) }

// NewMaterial creates a material and returns a ref-counted handle.
func (r *Renderer) NewMaterial(desc MaterialDesc) Material { return r.mats.newHandle(desc) }

// NewTexture uploads an RGBA8 image into the bindless heap and returns a handle.
func (r *Renderer) NewTexture(pixels []byte, w, h int) Texture { return r.texs.upload(pixels, w, h) }

// DefaultSampler returns the heap index of the default linear/repeat sampler.
func (r *Renderer) DefaultSampler() uint32 { return r.texs.DefaultSampler() }

// NewScene creates a scene bound to this renderer's backend.
func (r *Renderer) NewScene() *Scene { return NewScene(r.backend) }

// Backend exposes the underlying gpu backend (advanced/one-off use).
func (r *Renderer) Backend() gpu.Backend { return r.backend }

// Size returns the render target size in pixels.
func (r *Renderer) Size() (uint32, uint32) { return r.w, r.h }

// Aspect returns width/height.
func (r *Renderer) Aspect() float32 { return float32(r.w) / float32(r.h) }

// SetClearColor sets the color the framebuffer is cleared to each frame.
func (r *Renderer) SetClearColor(rgba glm.RGBA32F) { r.clear = rgba }

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
	if !r.hasTarget && r.sc.H == 0 {
		panic("renderer has no target")
	}
	r.Stats.StartFrame()
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

	if !r.hasTarget {
		// Windowed: AcquireNext blocks on vsync (not CPU cost) — outside the timing.
		target, _ := r.backend.AcquireNext(r.sc)
		encStart := time.Now()
		cl := r.backend.Begin()
		r.encode(cl, target, scene, planes, drawVP, eye)
		cpu += time.Since(encStart)
		r.Stats.AddCPUTime(cpu)
		r.backend.Present(r.sc, cl)
		r.readGPU()
		r.Stats.EndFrame()
		return
	}
	encStart := time.Now()
	cl := r.backend.Begin()
	r.encode(cl, r.target, scene, planes, drawVP, eye)
	cpu += time.Since(encStart)
	r.Stats.AddCPUTime(cpu)
	f := r.backend.Submit(cl)
	r.backend.Wait(f)
	r.readGPU()
	r.Stats.EndFrame()
}

// Capture copies the current render target into a CPU buffer and returns it as RGBA8
// (headless targets only; the target needs transfer usage). Call after Render.
func (r *Renderer) Capture() []byte {
	if !r.hasTarget {
		return nil
	}
	n := int(r.w * r.h * 4)
	if !r.readback.Valid() {
		r.readback = r.backend.Alloc(uint64(n), gpu.MemoryHost, "readback")
		r.pixels = make([]byte, n)
	}
	cl := r.backend.Begin()
	cl.CopyTextureToBuffer(r.readback, r.target, 0, 0)
	f := r.backend.Submit(cl)
	r.backend.Wait(f)
	copy(r.pixels, unsafe.Slice((*byte)(r.readback.Ptr), n))
	return r.pixels
}

// Pixels is an alias for Capture (headless).
func (r *Renderer) Pixels() []byte { return r.Capture() }

// encode records the cull + lit draw (and, when enabled, GPU timestamps + the HUD).
func (r *Renderer) encode(cl gpu.CommandList, target gpu.Texture, scene *Scene, planes [6][4]float32, drawVP glm.Mat4f, eye glm.Vec3f) {
	if r.showFPS {
		cl.ResetTimestamps(r.gpuPool, 2)
		cl.WriteTimestamp(r.gpuPool, 0, gpu.StageNone)
	}
	r.recordCull(cl, scene.dl, planes)
	cl.BeginRendering(gpu.RenderTargets{
		Color: []gpu.ColorAttachment{{Texture: target, Load: gpu.LoadClear, Clear: r.clear}},
		Depth: &gpu.DepthAttachment{Texture: r.depth, Load: gpu.LoadClear, Clear: 1.0},
	})
	cl.Viewport(0, 0, float32(r.w), float32(r.h), 0, 1)
	cl.Scissor(0, 0, int32(r.w), int32(r.h))
	r.recordDraw(cl, scene.dl, drawVP, eye, scene.lights.Addr())
	if r.showFPS && r.overlay != nil {
		r.overlay.draw(cl, float32(r.w), float32(r.h))
	}
	cl.EndRendering()
	if r.showFPS {
		cl.WriteTimestamp(r.gpuPool, 1, gpu.StageColorOutput)
	}
}

// syncScene uploads the renderer's shared tables + the scene's per-scene GPU state.
func (r *Renderer) syncScene(s *Scene) {
	r.geo.Sync()
	r.mats.Sync()
	s.UpdateTransforms()
	s.dl.uploadWorld(s.world)
	if s.drawableDirty {
		s.dl.rebuild(s.collectDrawables(), r.geo)
		s.drawableDirty = false
	}
	s.lights.Sync()
}

func (r *Renderer) recordCull(cl gpu.CommandList, dl *drawList, planes [6][4]float32) {
	if dl.batchCount() == 0 {
		return
	}
	writeAt(dl.indirectBuf, 0, toBytes(dl.template))
	*(*cullRoot)(dl.cullRootBuf.Ptr) = cullRoot{
		drawables: dl.drawableBuf.Addr, models: dl.worldBuf.Addr, indirect: dl.indirectBuf.Addr,
		regions: dl.regionBuf.Addr, visible: dl.visibleBuf.Addr, count: dl.numInst, planes: planes,
	}
	cl.SetPipeline(r.cullPipe)
	cl.Root(dl.cullRootBuf.Addr)
	cl.Dispatch((dl.numInst+63)/64, 1, 1)
	cl.Barrier(gpu.StageCompute, gpu.StageIndirect|gpu.StageVertex, 0)
}

func (r *Renderer) recordDraw(cl gpu.CommandList, dl *drawList, viewProj glm.Mat4f, eye glm.Vec3f, lightsAddr uint64) {
	if dl.batchCount() == 0 {
		return
	}
	roots := unsafe.Slice((*drawRoot)(dl.drawRootBuf.Ptr), len(dl.batches))
	idx := r.geo.IndexBuffer()
	cl.SetPipeline(r.drawPipe)
	for bid := range dl.batches {
		roots[bid] = drawRoot{
			viewProj: viewProj, pos: r.geo.PositionsAddr(), attr: r.geo.AttributesAddr(), descs: r.geo.DescriptorsAddr(),
			models: dl.worldBuf.Addr, drawables: dl.drawableBuf.Addr, visible: dl.visibleBuf.Addr,
			materials: r.mats.Addr(), lights: lightsAddr, regionBase: dl.batches[bid].regionBase,
			eye: [4]float32{eye[0], eye[1], eye[2], 1},
		}
		cl.Root(dl.drawRootBuf.Addr + uint64(bid)*drawRootSize)
		cl.DrawIndexedIndirect(idx, dl.indirectBuf, uint64(bid)*uint64(indirectSize), 1, indirectSize)
	}
}

func (r *Renderer) buildOverlay() {
	if r.overlay == nil {
		return
	}
	r.overlay.reset()
	ms := func(d time.Duration) float64 { return float64(d.Microseconds()) / 1000 }
	r.overlay.text(fmt.Sprintf("FPS %.0f", r.Stats.FPS()), 12, 12, 26, r.FontColor)
	r.overlay.text(fmt.Sprintf("CPU %.2f ms", ms(r.Stats.AvgCPUTime())), 12, 44, 26, r.FontColor)
	if r.gpuValid {
		r.overlay.text(fmt.Sprintf("GPU %.2f ms", ms(r.Stats.AvgGPUTime())), 12, 76, 26, r.FontColor)
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
	r.Stats.AddGPUTime(ns / 1e9)
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
		r.backend.DestroyPipeline(r.cullPipe)
		r.backend.DestroyPipeline(r.drawPipe)
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
	r.geo.Destroy()
	r.mats.Destroy()
	r.texs.Destroy()
	r.backend.Destroy()
}
