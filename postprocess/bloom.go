package postprocess

import (
	"github.com/bluescreen10/gamekit/gpu"
	"github.com/bluescreen10/gamekit/utils"
	"github.com/bluescreen10/pix/shaders"
)

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

// The roots of bloom's passes: Root, then each shader's own fields.

type bloomDownsampleRoot struct {
	Root
	firstLevel uint32
	_          [3]uint32
}

type bloomUpsampleRoot struct {
	Root
	radius float32
	_      [3]float32
}

type bloomCompositeRoot struct {
	Root
	bloom     uint32
	intensity float32
	_         [2]float32
}

// defaultBloomLevels is how many times the chain halves the image when Levels is zero:
// six levels reach about 1/64 of the screen from each light.
const defaultBloomLevels = 6

func (b *Bloom) Encode(frame *Frame, cmd gpu.CommandBuffer) {
	b.ensurePasses(frame.Backend)
	b.ensureChain(frame)

	// With nothing to add, the composite still runs: a step must write its target.
	bloom := frame.Source
	intensity := min(max(b.Intensity, 0), 1)
	if intensity > 0 && len(b.chain) > 0 {
		b.encodeChain(frame, cmd)
		bloom = b.chain[0].image
	}

	root := bloomCompositeRoot{
		Root:      frame.Root(frame.Source, frame.Width, frame.Height),
		bloom:     bloom.Index,
		intensity: intensity,
	}
	b.composite.Draw(frame.Target, frame.Width, frame.Height, gpu.LoadClear, utils.ToBytes(&root), cmd)
}

// encodeChain walks the chain down from the scene, then back up into its top level.
func (b *Bloom) encodeChain(frame *Frame, cmd gpu.CommandBuffer) {
	radius := b.Radius
	if radius <= 0 {
		radius = 1
	}

	source, width, height := frame.Source, frame.Width, frame.Height
	for i, level := range b.chain {
		root := bloomDownsampleRoot{Root: frame.Root(source, width, height)}
		if i == 0 {
			root.firstLevel = 1
		}
		b.downsample.Draw(level.image, level.width, level.height, gpu.LoadClear, utils.ToBytes(&root), cmd)
		// The next level samples this one, and the way back up blends into it.
		cmd.Barrier(gpu.StageColorOutput, gpu.StageFragment|gpu.StageColorOutput, 0)
		source, width, height = level.image, level.width, level.height
	}

	for i := len(b.chain) - 1; i > 0; i-- {
		smaller, larger := b.chain[i], b.chain[i-1]
		root := bloomUpsampleRoot{Root: frame.Root(smaller.image, smaller.width, smaller.height), radius: radius}
		b.upsample.Draw(larger.image, larger.width, larger.height, gpu.LoadKeep, utils.ToBytes(&root), cmd)
		cmd.Barrier(gpu.StageColorOutput, gpu.StageFragment|gpu.StageColorOutput, 0)
	}
}

// ensurePasses builds bloom's three passes on first use.
func (b *Bloom) ensurePasses(backend gpu.Backend) {
	if b.composite != nil {
		return
	}
	// Upsampling adds each smaller level into the one above it.
	additive := []gpu.BlendState{{Enable: true, ColorOp: gpu.BlendFactorOp{Src: gpu.BlendOne, Dst: gpu.BlendOne, Op: gpu.BlendAdd}, AlphaOp: gpu.BlendFactorOp{Src: gpu.BlendOne, Dst: gpu.BlendOneMinusSrcAlpha, Op: gpu.BlendAdd}}}
	b.backend = backend
	b.downsample = NewFullscreenPass(backend, FullscreenPassDescriptor{Fragment: shaders.BloomDownsample, Label: "bloom-downsample"})
	b.upsample = NewFullscreenPass(backend, FullscreenPassDescriptor{Fragment: shaders.BloomUpsample, Blend: additive, Label: "bloom-upsample"})
	b.composite = NewFullscreenPass(backend, FullscreenPassDescriptor{Fragment: shaders.BloomComposite, Label: "bloom-composite"})
}

// ensureChain makes sure the chain matches the requested depth and the frame's size,
// rebuilding it when either has changed. It stops early rather than halve an image below
// two pixels, so it may be shorter than asked — or empty, on a tiny target.
func (b *Bloom) ensureChain(frame *Frame) {
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
			image:  CreateImage(frame.Backend, width, height, "bloom"),
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
