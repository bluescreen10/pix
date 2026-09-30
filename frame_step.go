// Frame steps: work an application adds to the frame at one of its stages — a
// simulation, a sky, anything the renderer does not do itself.
package pix

import (
	"github.com/bluescreen10/gamekit/gpu"
	"github.com/bluescreen10/pix/glm"
)

// FrameStage is a point in the frame where frame steps run. Each is named for the work
// that has just finished there, which is what decides what a step can read.
type FrameStage uint8

const (
	// FrameStageStart runs before the renderer's own compute — skinning, culling and
	// particle simulation — so what a step writes there can feed it. Nothing of the
	// frame is drawn yet.
	FrameStageStart FrameStage = iota
	// FrameStageAfterShadows runs once the shadow maps are filled, before anything of the
	// main view is drawn.
	//
	// It and FrameStageStart run even when a debug view is shown or the scene has no
	// camera, so simulations keep advancing; the stages after it do not.
	FrameStageAfterShadows
	// FrameStageAfterDepth runs once the main view's depth is filled, before any shading.
	// Adding a step here turns the depth prepass on whatever EnableDepthPrepass says:
	// the prepass is what fills depth ahead of shading.
	FrameStageAfterDepth
	// FrameStageAfterOpaque runs once the opaque scene is shaded, before anything blended
	// is drawn over it.
	FrameStageAfterOpaque
	// FrameStageAfterTransparent runs once the whole scene is shaded, before the
	// post-processing chain.
	FrameStageAfterTransparent

	frameStageCount
)

// FrameStep is work added to the frame at one stage (see Renderer.AddFrameStep). It
// records whatever passes it needs, compute or draw, and may keep resources and
// pipelines of its own between frames.
type FrameStep interface {
	// Encode records the step's work for this frame. Encode orders its own passes
	// against each other; the renderer orders the stage as a whole against the rest of
	// the frame.
	Encode(frame *Frame, cmd gpu.CommandBuffer)
	// Release frees whatever the step created on the GPU. The renderer calls it when the
	// step is removed and when the renderer is destroyed; a step added again afterwards
	// recreates what it needs on its next Encode.
	Release()
}

// Frame is what a frame step is given: the frame's clock, the camera, and the scene
// images the stage has filled so far.
type Frame struct {
	// Number counts the frames of the scene being rendered, and Time is its clock in
	// seconds. DeltaTime is how far that clock moved since the renderer last rendered
	// the same scene; 0 the first time.
	Number    uint64
	Time      float32
	DeltaTime float32

	// ViewProj is the matrix the main view is drawn with — clip-space Y flipped, see
	// flipClipY — and InverseViewProj takes its clip space back to world space. Eye is
	// the camera's position. All three are zero when the scene has no camera, which
	// only FrameStageStart and FrameStageAfterShadows see.
	ViewProj        glm.Mat4f
	InverseViewProj glm.Mat4f
	Eye             glm.Vec3f

	// Width and Height are the size of the scene images, which is the render target's.
	Width, Height uint32

	// SceneDepth is the main view's depth: FormatDepth32F, reversed, so 1 is the near
	// plane and 0 the far one. From FrameStageAfterDepth on it is ready to sample, or to
	// attach read-only and depth-test against; before that it is zero. A step must not
	// write it.
	SceneDepth gpu.Texture
	// SceneColor is the image the scene is shaded into, in SceneColorFormat; from
	// FrameStageAfterOpaque on a step may draw into it, and before that it is zero. With
	// HDR on it is linear, unclamped light. With HDR off it is the render target itself,
	// already encoded for display.
	SceneColor       gpu.Texture
	SceneColorFormat gpu.Format

	// Backend is the GPU backend, for a step creating resources of its own.
	Backend gpu.Backend
	// LinearSampler is a linear, clamp-to-edge sampler.
	LinearSampler gpu.Sampler
}
