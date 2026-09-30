// Package postprocess is the post-processing chain's contract and its effects.
//
// The renderer runs a chain of Steps over the shaded scene of an HDR frame, before tone
// mapping (see pix.Renderer.SetPostProcessing). Each step reads the image the one before
// it produced and writes the next. The effects here — Bloom, Halftone, ShaderStep — are
// built on exactly the public API an application's own effect gets.
package postprocess

import (
	"github.com/bluescreen10/gamekit/gpu"
	"github.com/bluescreen10/gamekit/utils"
	"github.com/bluescreen10/pix/glm"
)

// Step is one effect in the chain. It records whatever passes it needs, and may keep
// images and pipelines of its own between frames.
type Step interface {
	// Encode records the effect: read frame.Source, write the result into frame.Target.
	// Both are linear, unclamped HDR images of frame.Width x frame.Height. The step must
	// write every pixel of Target, even when it has nothing to add.
	Encode(frame *Frame, cmd gpu.CommandBuffer)
	// Release frees whatever the step created on the GPU. The renderer calls it when the
	// step leaves the chain and when the renderer is destroyed; a step used again
	// afterwards recreates what it needs on its next Encode.
	Release()
}

// Frame is what a step is given each frame: the image to read, the image to write, and
// what every post-processing pass needs to read an image.
type Frame struct {
	// Source is the image so far — the shaded scene, or the previous step's result —
	// ready to sample. Target is where this step writes.
	Source gpu.Texture
	Target gpu.Texture
	// Width and Height are both images' size, which is the render target's.
	Width, Height uint32
	// Time is seconds since the scene's clock started.
	Time float32

	// ViewProj is the matrix the scene was drawn with — clip-space Y flipped, for
	// Vulkan's Y-down NDC — and InverseViewProj takes its clip space back to world
	// space. Eye is the camera's position.
	ViewProj        glm.Mat4f
	InverseViewProj glm.Mat4f
	Eye             glm.Vec3f
	// SceneDepth is the scene's depth, ready to sample: FormatDepth32F, reversed, so 1
	// is the near plane and 0 the far one. A step must not write it.
	SceneDepth gpu.Texture

	// Backend is the GPU backend, for a step creating resources of its own.
	Backend gpu.Backend
	// LinearSampler is a linear, clamp-to-edge sampler, the one every pass reads through.
	LinearSampler gpu.Sampler
}

// Root is the start of the root a pass reading image, which is width x height, pushes.
func (f *Frame) Root(image gpu.Texture, width, height uint32) Root {
	return Root{
		Source:        image.Index,
		LinearSampler: f.LinearSampler.Index,
		TexelSize:     glm.Vec2f{1 / float32(width), 1 / float32(height)},
		Time:          f.Time,
		SceneDepth:    f.SceneDepth.Index,
	}
}

// Root is POSTFX_ROOT in shaders/src/postfx.glsl: what every post-processing pass is told
// about the image it reads. A pass's own parameters follow it in the root.
type Root struct {
	// Source is the bindless index of the image to read, and LinearSampler of the
	// sampler to read it through. TexelSize is 1/size of Source. SceneDepth is the
	// bindless index of Frame.SceneDepth.
	Source        uint32
	LinearSampler uint32
	TexelSize     glm.Vec2f
	Time          float32
	SceneDepth    uint32
	_             [2]float32
}

// With returns the root with a pass's own parameters appended, padded to the multiple of
// 16 bytes the shader's block rounds up to.
func (root Root) With(params []byte) []byte {
	out := append(append([]byte(nil), utils.ToBytes(&root)...), params...)
	if pad := len(out) % 16; pad != 0 {
		out = append(out, make([]byte, 16-pad)...)
	}
	return out
}

// ImageFormat is what the scene is shaded into and the chain works in: half floats, so
// light above 1.0 survives until it is tone-mapped.
const ImageFormat = gpu.FormatRGBA16F

// CreateImage creates a linear HDR image of ImageFormat that a pass can render into and
// then sample: a step's intermediate result, or a chain of them. The caller owns it.
func CreateImage(backend gpu.Backend, width, height uint32, label string) gpu.Texture {
	return backend.CreateTexture(gpu.TextureDescriptor{
		Kind: gpu.Texture2D, Width: width, Height: height, Format: ImageFormat,
		Usage: gpu.TextureRenderTarget | gpu.TextureSampled, Label: label,
	})
}
