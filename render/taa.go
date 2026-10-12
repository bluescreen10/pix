package render

import (
	"github.com/bluescreen10/gamekit/gpu"
	"github.com/bluescreen10/gamekit/utils"
	"github.com/bluescreen10/pix/glm"
	"github.com/bluescreen10/pix/render/postprocess"
	"github.com/bluescreen10/pix/render/shaders"
)

// isTAAEnabled reports whether the frame is anti-aliased by TAA.
func (r *Renderer) isTAAEnabled() bool {
	return r.antiAliasingEnabled && r.antiAliasing == AntiAliasingTAA
}

// isProjectionJittered reports whether the main view is drawn from a different spot
// within the pixel each frame: while TAA is on, except under a geometry debug view, whose
// values TAA never blends and would only shake.
func (r *Renderer) isProjectionJittered() bool {
	return r.isTAAEnabled() && !r.isGeometryDebugViewActive()
}

// jitterSequenceLength is how many frames the jitter takes to cover the pixel before
// repeating.
const jitterSequenceLength = 8

// projectionJitter is where in the pixel frame number is drawn from, as an offset in
// clip space: a point of the Halton (2, 3) sequence, which spreads the frames' samples
// evenly over the pixel whatever stretch of the sequence is taken. Its first point, the
// pixel's corner, is skipped.
func projectionJitter(frame uint64, width, height uint32) glm.Vec2f {
	index := uint32(frame%jitterSequenceLength) + 1
	offset := glm.Vec2f{halton(index, 2) - 0.5, halton(index, 3) - 0.5}
	return glm.Vec2f{offset[0] * 2 / float32(width), offset[1] * 2 / float32(height)}
}

// halton is the index-th point of the Halton sequence in base, in [0, 1).
func halton(index, base uint32) float32 {
	point, scale := float32(0), float32(1)
	for index > 0 {
		scale /= float32(base)
		point += scale * float32(index%base)
		index /= base
	}
	return point
}

// jitterViewProj moves everything viewProj projects by jitter, in clip space: the same
// offset, after the perspective division, wherever a point lies.
func jitterViewProj(viewProj glm.Mat4f, jitter glm.Vec2f) glm.Mat4f {
	for column := range 4 {
		w := viewProj[column*4+3]
		viewProj[column*4] += jitter[0] * w
		viewProj[column*4+1] += jitter[1] * w
	}
	return viewProj
}

// prepareTAA makes sure TAA's images and passes exist while it is on — the histories in
// the scene's format, which is what TAA resolves — and frees them while it is off, so it
// starts again from no history when it comes back on.
func (r *Renderer) prepareTAA() {
	if !r.isTAAEnabled() {
		r.releaseTAA()
		return
	}
	if r.taaResolvePass != nil {
		return
	}
	for i := range r.taaHistories {
		r.taaHistories[i] = r.backend.CreateTexture(gpu.TextureDescriptor{Kind: gpu.Texture2D, Width: r.width, Height: r.height,
			Format: r.sceneFormat(), Usage: gpu.TextureRenderTarget | gpu.TextureSampled | gpu.TextureTransfer, Label: "taa-history"})
	}
	r.taaResolvePass = postprocess.NewFullscreenPass(r.backend, postprocess.FullscreenPassDescriptor{
		Fragment: shaders.TAAResolve, Format: r.sceneFormat(), Label: "taa-resolve",
	})
	if !r.hdr {
		r.taaSource = r.backend.CreateTexture(gpu.TextureDescriptor{Kind: gpu.Texture2D, Width: r.width, Height: r.height,
			Format: r.color, Usage: gpu.TextureRenderTarget | gpu.TextureSampled, Label: "taa-source"})
	}
}

// encodeTAA resolves source, the frame just shaded, against the history of the frames
// before it, and returns the resolve: next frame's history.
func (r *Renderer) encodeTAA(source gpu.Texture, cmd gpu.CommandBuffer) gpu.Texture {
	history := r.taaHistories[r.taaHistoryIndex]
	resolve := r.taaHistories[1-r.taaHistoryIndex]

	frame := r.postProcessingFrame(source, resolve)
	root := taaResolveRoot{
		Root:    frame.Root(source, r.width, r.height),
		history: history.Index,
		motion:  r.sceneMotion.Index,
	}
	if r.hasTAAHistory {
		root.hasHistory = 1
	}
	r.taaResolvePass.Draw(resolve, r.width, r.height, gpu.LoadDontCare, utils.ToBytes(&root), cmd)

	r.taaHistoryIndex = 1 - r.taaHistoryIndex
	r.hasTAAHistory = true
	return resolve
}

// taaResolveRoot is the root of taa_resolve.frag.
type taaResolveRoot struct {
	postprocess.Root
	history    uint32
	motion     uint32
	hasHistory uint32
	_          uint32
}

// encodeDisplayTAA resolves the frame without HDR: drawn into taaSource, as it will be
// displayed, it is resolved into the history and copied from there into target, which
// is in the same format. With HDR TAA runs at the head of the post-processing chain
// instead.
func (r *Renderer) encodeDisplayTAA(target gpu.Texture, cmd gpu.CommandBuffer) {
	if !r.isTAAEnabled() || r.hdr {
		return
	}

	cmd.Barrier(gpu.StageColorOutput|gpu.StageDepth, gpu.StageFragment, 0)
	resolve := r.encodeTAA(r.taaSource, cmd)
	cmd.Barrier(gpu.StageColorOutput, gpu.StageTransfer, 0)
	cmd.CopyTextureToTexture(target, resolve, 0, 0)
	// The overlay draws over the frame next.
	cmd.Barrier(gpu.StageTransfer, gpu.StageColorOutput, 0)
}

// releaseTAA frees TAA's images and passes, and with them its history.
func (r *Renderer) releaseTAA() {
	for _, image := range []*gpu.Texture{&r.taaHistories[0], &r.taaHistories[1], &r.taaSource} {
		if image.IsValid() {
			r.backend.DestroyTexture(*image)
		}
		*image = gpu.Texture{}
	}
	if r.taaResolvePass != nil {
		r.taaResolvePass.Release()
		r.taaResolvePass = nil
	}
	r.hasTAAHistory = false
}
