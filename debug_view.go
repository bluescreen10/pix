package pix

import (
	"github.com/bluescreen10/pix/glm"
	"github.com/bluescreen10/pix/gpu"
	"github.com/bluescreen10/pix/shaders"
)

// DebugView selects a G-buffer target to display fullscreen in place of the shaded
// frame — the "what is the geometry pass actually writing?" question, answered without
// a graphics debugger.
//
// It only applies while deferred rendering is on, because it is the deferred path's
// intermediate targets that it shows; forward rendering never fills them.
type DebugView uint32

const (
	DebugOff      DebugView = iota // shade normally
	DebugAlbedo                    // base colour
	DebugNormal                    // world normals, decoded and remapped to [0,1]
	DebugMaterial                  // metallic / roughness / occlusion channels
	DebugEmissive                  // emitted light
	DebugDepth                     // depth, inverted and curved for readability
	DebugPosition                  // world position reconstructed from depth, fractional
)

// debugViewNames is the console/round-trip spelling of each view, in enum order.
var debugViewNames = [...]string{
	"off", "albedo", "normal", "material", "emissive", "depth", "position",
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
func (r *Renderer) DebugView() DebugView { return r.debugView }

// SetDebugView displays one G-buffer target fullscreen instead of the shaded frame.
// DebugOff restores normal shading.
//
// Takes effect on the next Render. It has no effect unless deferred rendering is
// enabled — see EnableDeferredRendering — since these are the deferred path's targets.
func (r *Renderer) SetDebugView(v DebugView) {
	r.debugView = v
}

// debugViewActive reports whether this frame should show a G-buffer target instead of
// the shaded result.
func (r *Renderer) debugViewActive() bool {
	return r.debugView != DebugOff && r.deferredEnabled
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
		r.debugPipeline = r.buildLightingPipe(shaders.GBufferDebug)
	}
	*(*lightingRoot)(dl.lightingRootBuf.Ptr) = lightingRoot{
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
		Color: []gpu.ColorAttachment{{Texture: target, Load: gpu.LoadClear, Clear: r.clear}},
	})
	cmd.Viewport(0, 0, float32(r.width), float32(r.height), 0, 1)
	cmd.Scissor(0, 0, int32(r.width), int32(r.height))
	cmd.SetPipeline(r.debugPipeline)
	cmd.Root(dl.lightingRootBuf.Addr)
	cmd.Draw(3, 1, 0, 0)
	cmd.EndRenderPass()
}
