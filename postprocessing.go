package pix

import (
	"github.com/bluescreen10/gamekit/gpu"
	"github.com/bluescreen10/gamekit/utils"
	"github.com/bluescreen10/pix/glm"
	"github.com/bluescreen10/pix/materials"
	"github.com/bluescreen10/pix/shaders"
)

// PostProcessingStep is one effect in the chain the renderer runs over the shaded scene
// of an HDR frame (see Renderer.SetPostProcessing).
//
// Bloom is built on exactly this interface and the public helpers it is
// given, so an application's own effect has everything they have: it records whatever
// passes it needs, and may keep images and pipelines of its own between frames.
type PostProcessingStep interface {
	// Encode records the effect: read frame.Source, write the result into frame.Target.
	// Both are linear, unclamped HDR images of frame.Width x frame.Height. The step must
	// write every pixel of Target, even when it has nothing to add.
	Encode(frame *PostProcessingFrame, cmd gpu.CommandBuffer)
	// Release frees whatever the step created on the GPU. The renderer calls it when the
	// step leaves the chain and when the renderer is destroyed; a step used again
	// afterwards recreates what it needs on its next Encode.
	Release()
}

// PostProcessingFrame is what a step is given each frame: the image to read, the image to
// write, and the renderer resources every post-processing pass needs.
type PostProcessingFrame struct {
	// Source is the image so far — the shaded scene, or the previous step's result —
	// ready to sample. Target is where this step writes.
	Source gpu.Texture
	Target gpu.Texture
	// Width and Height are both images' size, which is the target's.
	Width, Height uint32
	// Time is seconds since the scene's clock started.
	Time float32

	renderer *Renderer
}

// Backend is the GPU backend, for a step creating resources of its own.
func (f *PostProcessingFrame) Backend() gpu.Backend {
	return f.renderer.backend
}

// Root is the start of the root every post-processing pass pushes — POSTFX_ROOT in
// shaders/src/postfx.glsl — for a pass reading image, which is width x height.
func (f *PostProcessingFrame) Root(image gpu.Texture, width, height uint32) PostProcessingRoot {
	return f.renderer.postProcessingRoot(image, width, height)
}

// CreateImage creates a linear HDR image a step can render into and then sample: an
// intermediate result, or a chain of them. The step owns it and destroys it in Release.
func (f *PostProcessingFrame) CreateImage(width, height uint32, label string) gpu.Texture {
	return f.renderer.createHDRImage(width, height, label)
}

// PostProcessingRoot is POSTFX_ROOT in shaders/src/postfx.glsl: what every
// post-processing pass is told about the image it reads. A pass's own parameters follow
// it in the root.
type PostProcessingRoot struct {
	// Source is the bindless index of the image to read, and LinearSampler of a linear,
	// clamp-to-edge sampler. TexelSize is 1/size of Source.
	Source        uint32
	LinearSampler uint32
	TexelSize     glm.Vec2f
	Time          float32
	_             [3]float32
}

// With returns the root with a pass's own parameters appended, padded to the multiple of
// 16 bytes the shader's block rounds up to.
func (root PostProcessingRoot) With(params []byte) []byte {
	out := append(append([]byte(nil), utils.ToBytes(&root)...), params...)
	if pad := len(out) % 16; pad != 0 {
		out = append(out, make([]byte, 16-pad)...)
	}
	return out
}

// FullscreenPass draws one full-screen triangle through a fragment shader into a linear
// HDR image: the building block of every post-processing effect.
type FullscreenPass struct {
	backend  gpu.Backend
	pipeline gpu.Pipeline
}

// NewFullscreenPass builds a pass for a fragment shader. fragment is backend-native
// bytes; its root starts with POSTFX_ROOT. blend is nil to replace what is in the target,
// or a blend state to combine with it.
func NewFullscreenPass(backend gpu.Backend, fragment []byte, blend []gpu.BlendState, label string) *FullscreenPass {
	return newFullscreenPass(backend, fragment, hdrColorFormat, blend, label)
}

// newFullscreenPass builds a pass into any colour format — the tone-map pass writes the
// final target's.
func newFullscreenPass(backend gpu.Backend, fragment []byte, format gpu.Format, blend []gpu.BlendState, label string) *FullscreenPass {
	pipeline := backend.CreateGraphicsPipeline(gpu.PipelineDescriptor{
		VertexShader:   shaders.ForBackend(backend, shaders.FullscreenVert),
		FragmentShader: shaders.ForBackend(backend, fragment),
		Topology:       gpu.TopologyTriangles,
		ColorFormats:   []gpu.Format{format},
		Blend:          blend,
		CullMode:       gpu.CullNone,
		Label:          label,
	})
	return &FullscreenPass{backend: backend, pipeline: pipeline}
}

// Draw runs the pass into target, which is width x height, in a render pass of its own.
// load is LoadClear or LoadDontCare to overwrite the target, LoadKeep to blend into it.
func (p *FullscreenPass) Draw(target gpu.Texture, width, height uint32, load gpu.LoadOp, root []byte, cmd gpu.CommandBuffer) {
	cmd.BeginRenderPass(gpu.RenderTargets{
		Color: []gpu.ColorAttachment{{Texture: target, Load: load, Store: gpu.StoreKeep}},
	})
	cmd.SetViewport(0, 0, float32(width), float32(height), 0, 1)
	cmd.SetScissor(0, 0, int32(width), int32(height))
	cmd.SetPipeline(p.pipeline)
	cmd.Draw(root, 3, 1, 0, 0)
	cmd.EndRenderPass()
}

// Release frees the pass's pipeline.
func (p *FullscreenPass) Release() {
	p.backend.DestroyPipeline(p.pipeline)
}

// ---------------------------------------------------------------------------------------
// Bloom
// ---------------------------------------------------------------------------------------

// Bloom spreads the brightest light in the scene into a soft glow around it, the way a
// camera lens or an eye does.
//
// It is physically based rather than thresholded: every pixel contributes in proportion
// to its brightness, so only light well above white glows noticeably. A scene whose
// lighting never exceeds 1.0 shows almost none — which is what HDR lighting is for. The
// chain runs before tone mapping, which compresses exactly the light bloom works from.
//
// The technique is Jimenez's, from "Next Generation Post Processing in Call of Duty:
// Advanced Warfare": walk a chain of images down, each half the size of the one before,
// blurring as it goes; walk it back up, adding each level into the one above; blend the
// top level into the scene.
type Bloom struct {
	// Intensity is how much of the image the bloom replaces. Around 0.04 is subtle and
	// 0.1 strong; zero draws no bloom.
	Intensity float32
	// Radius widens each level's blur, in texels of that level. Zero uses 1.
	Radius float32
	// Levels is how many times the chain halves the image. More levels reach further
	// from each light. Zero uses 6.
	Levels int

	backend    gpu.Backend
	downsample *FullscreenPass
	upsample   *FullscreenPass
	composite  *FullscreenPass
	chain      []bloomLevel
}

// bloomLevel is one image of the bloom chain.
type bloomLevel struct {
	image         gpu.Texture
	width, height uint32
}

// The roots of bloom's passes: PostProcessingRoot, then each shader's own fields.

type bloomDownsampleRoot struct {
	PostProcessingRoot
	firstLevel uint32
	_          [3]uint32
}

type bloomUpsampleRoot struct {
	PostProcessingRoot
	radius float32
	_      [3]float32
}

type bloomCompositeRoot struct {
	PostProcessingRoot
	bloom     uint32
	intensity float32
	_         [2]float32
}

// defaultBloomLevels is how many times the chain halves the image when Levels is zero:
// six levels reach about 1/64 of the screen from each light.
const defaultBloomLevels = 6

func (b *Bloom) Encode(frame *PostProcessingFrame, cmd gpu.CommandBuffer) {
	b.ensurePasses(frame.Backend())
	b.ensureChain(frame)

	// With nothing to add, the composite still runs: a step must write its target.
	bloom := frame.Source
	intensity := min(max(b.Intensity, 0), 1)
	if intensity > 0 && len(b.chain) > 0 {
		b.encodeChain(frame, cmd)
		bloom = b.chain[0].image
	}

	root := bloomCompositeRoot{
		PostProcessingRoot: frame.Root(frame.Source, frame.Width, frame.Height),
		bloom:              bloom.Index,
		intensity:          intensity,
	}
	b.composite.Draw(frame.Target, frame.Width, frame.Height, gpu.LoadClear, utils.ToBytes(&root), cmd)
}

// encodeChain walks the chain down from the scene, then back up into its top level.
func (b *Bloom) encodeChain(frame *PostProcessingFrame, cmd gpu.CommandBuffer) {
	radius := b.Radius
	if radius <= 0 {
		radius = 1
	}

	source, width, height := frame.Source, frame.Width, frame.Height
	for i, level := range b.chain {
		root := bloomDownsampleRoot{PostProcessingRoot: frame.Root(source, width, height)}
		if i == 0 {
			root.firstLevel = 1
		}
		b.downsample.Draw(level.image, level.width, level.height, gpu.LoadClear, utils.ToBytes(&root), cmd)
		cmd.PrepareSampled(level.image, gpu.StageFragment)
		source, width, height = level.image, level.width, level.height
	}

	for i := len(b.chain) - 1; i > 0; i-- {
		smaller, larger := b.chain[i], b.chain[i-1]
		root := bloomUpsampleRoot{PostProcessingRoot: frame.Root(smaller.image, smaller.width, smaller.height), radius: radius}
		b.upsample.Draw(larger.image, larger.width, larger.height, gpu.LoadKeep, utils.ToBytes(&root), cmd)
		cmd.PrepareSampled(larger.image, gpu.StageFragment)
	}
}

// ensurePasses builds bloom's three passes on first use.
func (b *Bloom) ensurePasses(backend gpu.Backend) {
	if b.composite != nil {
		return
	}
	// Upsampling adds each smaller level into the one above it.
	additive := []gpu.BlendState{{Enable: true, ColorOp: gpu.BlendFactorOp{Src: gpu.BlendOne, Dst: gpu.BlendOne, Op: gpu.BlendAdd}}}
	b.backend = backend
	b.downsample = NewFullscreenPass(backend, shaders.BloomDownsample, nil, "bloom-downsample")
	b.upsample = NewFullscreenPass(backend, shaders.BloomUpsample, additive, "bloom-upsample")
	b.composite = NewFullscreenPass(backend, shaders.BloomComposite, nil, "bloom-composite")
}

// ensureChain makes sure the chain matches the requested depth and the frame's size,
// rebuilding it when either has changed. It stops early rather than halve an image below
// two pixels, so it may be shorter than asked — or empty, on a tiny target.
func (b *Bloom) ensureChain(frame *PostProcessingFrame) {
	levels := b.Levels
	if levels <= 0 {
		levels = defaultBloomLevels
	}
	width, height := frame.Width/2, frame.Height/2
	if len(b.chain) > 0 && b.chain[0].width == width && b.chain[0].height == height && len(b.chain) == levels {
		return
	}

	b.releaseChain()
	for range levels {
		if width < 2 || height < 2 {
			break
		}
		b.chain = append(b.chain, bloomLevel{
			image:  frame.CreateImage(width, height, "bloom"),
			width:  width,
			height: height,
		})
		width, height = width/2, height/2
	}
}

func (b *Bloom) releaseChain() {
	for _, level := range b.chain {
		b.backend.DestroyTexture(level.image)
	}
	b.chain = nil
}

func (b *Bloom) Release() {
	if b.composite == nil {
		return
	}
	b.releaseChain()
	for _, pass := range []*FullscreenPass{b.downsample, b.upsample, b.composite} {
		pass.Release()
	}
	b.downsample, b.upsample, b.composite = nil, nil, nil
}

// ---------------------------------------------------------------------------------------
// Tone mapping
// ---------------------------------------------------------------------------------------

// ToneMapOperator is the curve an HDR frame's light is mapped through into the range a
// display can show (see Renderer.SetToneMapping). Every curve but ToneMapNone compresses
// highlights instead of clipping them.
type ToneMapOperator uint32

const (
	// ToneMapNeutral is Khronos PBR Neutral: it leaves colours below its compression
	// point almost untouched, so a material's base colour survives to the display, and
	// rolls highlights off towards white.
	ToneMapNeutral ToneMapOperator = iota
	// ToneMapACESFitted is Stephen Hill's fit of the ACES reference rendering and output
	// transforms for an sRGB display: filmic contrast and slightly desaturated brights.
	// It is a fit of the look, not a versioned ACES transform.
	ToneMapACESFitted
	// ToneMapReinhard is x/(1+x): simple, and keeps hues, but flattens contrast.
	ToneMapReinhard
	// ToneMapNone clips everything brighter than white, as a frame without HDR does.
	ToneMapNone
)

// toneMapRoot is the root of the tone-map pass, shaders/src/tonemap.frag.glsl.
type toneMapRoot struct {
	PostProcessingRoot
	operator ToneMapOperator
	exposure float32 // in stops
	_        [2]float32
}

// ---------------------------------------------------------------------------------------
// ShaderStep
// ---------------------------------------------------------------------------------------

// ShaderStep is the simplest effect: one full-screen pass of a fragment shader, from the
// image so far into the next. An effect that needs more than one pass implements
// PostProcessingStep itself, the way Bloom does.
//
// Fragment is backend-native bytes; its root starts with POSTFX_ROOT, and Params follows
// it, laid out as the shader declares its own fields. Params may change between frames,
// and a replaced Fragment is picked up on the next one.
type ShaderStep struct {
	Fragment []byte
	Params   []byte
	Label    string

	pass     *FullscreenPass
	fragment []byte // what pass was built from, so a replaced Fragment is noticed
}

func (s *ShaderStep) Encode(frame *PostProcessingFrame, cmd gpu.CommandBuffer) {
	if s.pass == nil || !materials.SameSPIRV(s.fragment, s.Fragment) {
		s.Release()
		label := s.Label
		if label == "" {
			label = "post-shader-step"
		}
		s.pass = NewFullscreenPass(frame.Backend(), s.Fragment, nil, label)
		s.fragment = s.Fragment
	}
	root := frame.Root(frame.Source, frame.Width, frame.Height)
	s.pass.Draw(frame.Target, frame.Width, frame.Height, gpu.LoadClear, root.With(s.Params), cmd)
}

func (s *ShaderStep) Release() {
	if s.pass != nil {
		s.pass.Release()
		s.pass = nil
	}
}
