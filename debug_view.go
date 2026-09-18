package pix

import (
	"github.com/bluescreen10/gamekit/gpu"
	"github.com/bluescreen10/gamekit/utils"
	"github.com/bluescreen10/pix/glm"
	"github.com/bluescreen10/pix/shaders"
)

// DebugView selects either a G-buffer target or a standalone id-color pass to display
// fullscreen in place of the shaded frame — the "what is the geometry pass actually
// writing?" question, answered without a graphics debugger.
//
// DebugAlbedo through DebugPosition only apply while deferred rendering is on, because
// they show the deferred path's own intermediate targets; forward rendering never
// fills them. DebugObjectID and DebugTriangleID are a genuinely separate mechanism —
// a small dedicated draw pass (see recordDebugIDView) — and work regardless of
// forward/deferred mode, since object/triangle identity isn't sitting in any existing
// G-buffer channel to just re-read.
type DebugView uint32

const (
	DebugOff        DebugView = iota // shade normally
	DebugAlbedo                      // base colour
	DebugNormal                      // world normals, decoded and remapped to [0,1]
	DebugMaterial                    // metallic / roughness / occlusion channels
	DebugEmissive                    // emitted light
	DebugDepth                       // depth, inverted and curved for readability
	DebugPosition                    // world position reconstructed from depth, fractional
	DebugObjectID                    // one flat color per drawable (a palette lookup, not raw hash-to-RGB)
	DebugTriangleID                  // one flat color per triangle within a drawable, from the same palette
)

// debugViewNames is the console/round-trip spelling of each view, in enum order.
var debugViewNames = [...]string{
	"off", "albedo", "normal", "material", "emissive", "depth", "position", "objectid", "triangleid",
}

// String returns the view's name ("off", "albedo", …).
func (v DebugView) String() string {
	if int(v) < len(debugViewNames) {
		return debugViewNames[v]
	}
	return "off"
}

// ParseDebugView resolves a view by name. The second result reports whether the name
// was known.
func ParseDebugView(s string) (DebugView, bool) {
	for i, name := range debugViewNames {
		if name == s {
			return DebugView(i), true
		}
	}
	return DebugOff, false
}

// DebugViewNames lists every accepted name, for help text and completion.
func DebugViewNames() []string {
	return debugViewNames[:]
}

// DebugView reports which G-buffer target is being displayed.
func (r *Renderer) DebugView() DebugView {
	return r.debugView
}

// SetDebugView displays one G-buffer target, or the object/triangle id pass,
// fullscreen instead of the shaded frame. DebugOff restores normal shading.
//
// Takes effect on the next Render. DebugAlbedo..DebugPosition have no effect unless
// deferred rendering is enabled — see EnableDeferredRendering — since those are the
// deferred path's own targets; DebugObjectID/DebugTriangleID always work (see
// DebugView's doc comment).
func (r *Renderer) SetDebugView(v DebugView) {
	r.debugView = v
}

// debugViewActive reports whether this frame should show a G-buffer target instead of
// the shaded result. Object/triangle id views are handled separately (see
// idViewActive) — they don't need deferred rendering on at all.
func (r *Renderer) debugViewActive() bool {
	return r.debugView != DebugOff && r.debugView < DebugObjectID && r.deferredEnabled
}

// idViewActive reports whether this frame should run the standalone object/triangle
// id pass (see recordDebugIDView) instead of normal shading.
func (r *Renderer) idViewActive() bool {
	return r.debugView == DebugObjectID || r.debugView == DebugTriangleID
}

// recordDebugView draws the selected G-buffer target over the whole frame, in place of
// the deferred lighting pass. The forward pass that follows it in encode draws the
// overlay over the top, so the console stays usable while a view is up — which is the
// only way to turn one off again.
//
// It reuses the deferred lighting pass's root struct and its already-populated
// contents, so this is one extra fullscreen draw reading buffers the frame produced
// anyway — nothing about the geometry pass changes when a view is on.
func (r *Renderer) recordDebugView(cmd gpu.CommandBuffer, dl *drawList, target gpu.Texture, viewProj glm.Mat4f, eye glm.Vec3f, lightsAddr uint64) {
	if r.debugPipeline.H == 0 {
		r.debugPipeline = r.backend.CreateGraphicsPipeline(gpu.PipelineDescriptor{
			VertexShader:   shaders.ForBackend(r.backend, shaders.FullscreenVert),
			FragmentShader: shaders.ForBackend(r.backend, shaders.GBufferDebug),
			Topology:       gpu.TopologyTriangles, ColorFormats: []gpu.Format{r.color},
			CullMode: gpu.CullNone, Label: "debug-view",
		})
	}
	lr := lightingRoot{
		invViewProj:     viewProj.Inv(),
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
		debugView:       uint32(r.debugView),
	}
	// LoadClear, not LoadKeep: this replaces the frame rather than compositing over
	// it, and the depth test is disabled for the same reason — a G-buffer target is
	// screen-space data, not geometry to be occluded.
	cmd.BeginRenderPass(gpu.RenderTargets{
		Color: []gpu.ColorAttachment{{Texture: target, Load: gpu.LoadClear, Store: gpu.StoreKeep, Clear: r.clear}},
	})
	cmd.SetViewport(0, 0, float32(r.width), float32(r.height), 0, 1)
	cmd.SetScissor(0, 0, int32(r.width), int32(r.height))
	cmd.SetPipeline(r.debugPipeline)
	cmd.Draw(utils.ToBytes(&lr), 3, 1, 0, 0)
	cmd.EndRenderPass()
}

// recordDebugIDView is a self-contained real geometry pass (its own depth test/write,
// not a re-read of already-rendered data) that colors every visible triangle by
// either its drawable's index or its own gl_PrimitiveID, via scene_debug_id.vert/
// .frag — see DebugView's doc comment for why this can't just be another G-buffer
// re-read like the other views. It draws every batch through one shared pipeline in
// a single multi-draw-indirect call (spanning dl.template/dl.indirectBuf in full,
// rather than one call per pipeline run the way issueDraws does), since every batch
// uses the same debug pipeline regardless of its material's own one.
func (r *Renderer) recordDebugIDView(cmd gpu.CommandBuffer, dl *drawList, target gpu.Texture, viewProj glm.Mat4f) {
	if r.debugIDPipeline.H == 0 {
		r.debugIDPipeline = r.backend.CreateGraphicsPipeline(gpu.PipelineDescriptor{
			VertexShader: shaders.ForBackend(r.backend, shaders.SceneDebugIDVert), FragmentShader: shaders.ForBackend(r.backend, shaders.SceneDebugIDFrag),
			Topology: gpu.TopologyTriangles, ColorFormats: []gpu.Format{r.color},
			DepthFormat: gpu.FormatDepth32F, DepthTest: true, DepthWrite: true, DepthCompare: gpu.CompareGreater,
			// CullNone, not CullBack: a drawable's own material may be double-sided
			// (CullNone) or front-culled, and this pass has no per-drawable way to
			// know which — hardcoding CullBack incorrectly dropped every backface of
			// any double-sided object (foliage, glass, ...), showing as missing
			// geometry. There's no lighting-correctness reason to cull here at all
			// (unlike normal shading), so just draw every triangle regardless of
			// winding.
			CullMode: gpu.CullNone, FrontFaceCW: true, Label: "debug-id",
		})
	}
	mode := uint32(0)
	if r.debugView == DebugTriangleID {
		mode = 1
	}
	root := debugIDRoot{
		viewProj:  viewProj,
		pos:       r.GeometryStore.PositionsAddr(),
		descs:     r.GeometryStore.DescriptorsAddr(),
		models:    dl.worldBuf.Addr,
		drawables: dl.drawableBuf.Addr,
		visible:   dl.visibleBuf.Addr,
		mode:      mode,
	}
	cmd.BeginRenderPass(gpu.RenderTargets{
		Color: []gpu.ColorAttachment{{Texture: target, Load: gpu.LoadClear, Store: gpu.StoreKeep, Clear: r.clear}},
		Depth: &gpu.DepthAttachment{Texture: r.depth, Load: gpu.LoadClear, Store: gpu.StoreKeep, Clear: 0.0},
	})
	cmd.SetViewport(0, 0, float32(r.width), float32(r.height), 0, 1)
	cmd.SetScissor(0, 0, int32(r.width), int32(r.height))
	if len(dl.batches) > 0 {
		cmd.SetPipeline(r.debugIDPipeline)
		idx := r.GeometryStore.IndexBuffer()
		cmd.DrawIndexedIndirect(utils.ToBytes(&root), idx, gpu.IndexUint32, dl.indirectBuf, 0, uint32(len(dl.batches)), indirectSize)
	}
	cmd.EndRenderPass()
}
