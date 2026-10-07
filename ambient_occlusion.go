package pix

import (
	"github.com/bluescreen10/gamekit/gpu"
	"github.com/bluescreen10/pix/glm"
	"github.com/bluescreen10/pix/textures"
)

// AmbientOcclusion is how the renderer measures ambient occlusion — how much of the
// light around a surface reaches it, which creases, corners and the ground beneath an
// object do not let all in (see Renderer.SetAmbientOcclusion). It takes effect only
// while ambient occlusion is enabled (Renderer.EnableAmbientOcclusion).
//
// However it is measured, occlusion darkens only indirect light: ambient light and the
// environment's. Direct light has shadows to decide where it does not reach.
type AmbientOcclusion uint8

const (
	// AmbientOcclusionVBAO measures occlusion from the scene's depth alone, once the
	// opaque scene is drawn — no extra geometry pass, and no depth prepass — by
	// visibility bitmask ambient occlusion (Therrien, Levesque and Gilet, 2023) on the
	// slices of ground-truth ambient occlusion as Intel's XeGTAO lays them out: a few
	// directions per pixel, and along each a few samples either way, each hiding the
	// part of the sky between its front and its back, the settings' Thickness behind.
	// What is behind a thin thing stays in view. Each frame is measured on its own and
	// smoothed within surfaces, with no history, so nothing trails behind what moves.
	// What it measures is the share of the sky a point sees, so a surface reads the same
	// from near as from far. It is the default.
	AmbientOcclusionVBAO AmbientOcclusion = iota
	// AmbientOcclusionASSAO is Intel's Adaptive SSAO: the depth split into four
	// interleaved images a quarter (half, at full resolution) the size across,
	// mirrored pairs of taps gathered in each with a sampling pattern of its own, each
	// image blurred, and the four put back together. Each tap obscures by how far it
	// rises above the surface, so occlusion falls off smoothly, with no bands, from few
	// samples and no history — nothing trails behind moving objects. It is the cheaper
	// method, for mobile GPUs. What it gives up is measuring the sky a point sees: a
	// surface reads darker seen from close by than from far.
	AmbientOcclusionASSAO
)

// ambientOcclusionNames is the console/round-trip spelling of each method, in enum order.
var ambientOcclusionNames = [...]string{"vbao", "assao"}

// String returns the method's name ("vbao", "assao").
func (a AmbientOcclusion) String() string {
	if int(a) < len(ambientOcclusionNames) {
		return ambientOcclusionNames[a]
	}
	return ambientOcclusionNames[AmbientOcclusionVBAO]
}

// ParseAmbientOcclusion resolves a method by name. The second result reports whether the
// name was known.
func ParseAmbientOcclusion(name string) (AmbientOcclusion, bool) {
	for i, known := range ambientOcclusionNames {
		if known == name {
			return AmbientOcclusion(i), true
		}
	}
	return AmbientOcclusionVBAO, false
}

// AmbientOcclusionNames lists every method's name, in enum order.
func AmbientOcclusionNames() []string {
	return ambientOcclusionNames[:]
}

// AmbientOcclusionQuality is how many samples ambient occlusion takes, and how much it
// smooths them: higher is less grainy, and costs more (see ambientOcclusionQualities).
type AmbientOcclusionQuality uint8

const (
	// AmbientOcclusionQualityLow is the fewest samples: grain shows where occlusion
	// changes, the least it costs.
	AmbientOcclusionQualityLow AmbientOcclusionQuality = iota + 1
	// AmbientOcclusionQualityMedium is the default.
	AmbientOcclusionQualityMedium
	// AmbientOcclusionQualityHigh is the most samples and smoothing, for no visible
	// grain.
	AmbientOcclusionQualityHigh
)

// ambientOcclusionQualityNames is the console spelling of each quality, in enum order
// from Low.
var ambientOcclusionQualityNames = [...]string{"low", "medium", "high"}

// String returns the quality's name ("low", "medium", "high").
func (q AmbientOcclusionQuality) String() string {
	if q >= AmbientOcclusionQualityLow && int(q) <= len(ambientOcclusionQualityNames) {
		return ambientOcclusionQualityNames[q-1]
	}
	return ambientOcclusionQualityNames[AmbientOcclusionQualityMedium-1]
}

// ParseAmbientOcclusionQuality resolves a quality by name. The second result reports
// whether the name was known.
func ParseAmbientOcclusionQuality(name string) (AmbientOcclusionQuality, bool) {
	for i, known := range ambientOcclusionQualityNames {
		if known == name {
			return AmbientOcclusionQuality(i + 1), true
		}
	}
	return AmbientOcclusionQualityMedium, false
}

// AmbientOcclusionQualityNames lists every quality's name, from Low.
func AmbientOcclusionQualityNames() []string {
	return ambientOcclusionQualityNames[:]
}

// occlusionSampling is what a quality sets for each method.
type occlusionSampling struct {
	// slices and steps are how many directions VBAO measures each texel along, and how
	// many samples it takes each way along each; denoisePasses how many times it then
	// smooths them (see vbao_slices.comp.glsl and vbao_denoise.comp.glsl).
	slices, steps, denoisePasses int
	// assaoTaps is how many mirrored pairs of taps ASSAO gathers each texel with, of its
	// sample pattern's twelve, and assaoBlurPasses how many times it blurs them — an even
	// number: the blurs go back and forth between two images, and the darkening draw reads
	// the first.
	assaoTaps, assaoBlurPasses int
}

// ambientOcclusionQualities is what each quality samples with, by AmbientOcclusionQuality
// from Low. VBAO's are XeGTAO's presets a step up — its Medium (2 slices of 2 steps) is
// our Low — as XeGTAO leaves what grain they leave to temporal antialiasing, and pix has
// none. Medium is what pix used before qualities: 5 denoise passes, as 3 left the ao
// room's floor grainy at 4x intensity. ASSAO's taps stop at its pattern's twelve, so its
// High blurs more instead.
var ambientOcclusionQualities = [...]occlusionSampling{
	{slices: 2, steps: 2, denoisePasses: 3, assaoTaps: 6, assaoBlurPasses: 2},
	{slices: 3, steps: 3, denoisePasses: 5, assaoTaps: 12, assaoBlurPasses: 2},
	{slices: 6, steps: 4, denoisePasses: 5, assaoTaps: 12, assaoBlurPasses: 4},
}

// sampling is what the settings' quality samples with.
func (s AmbientOcclusionSettings) sampling() occlusionSampling {
	return ambientOcclusionQualities[s.resolved().Quality-1]
}

// AmbientOcclusionSettings is what ambient occlusion looks like, whichever method
// measures it. A zero field takes its default, so the zero value is a sensible setting.
type AmbientOcclusionSettings struct {
	// Radius is how far apart, in world units, two surfaces can be and still occlude
	// one another. 0 means 0.5.
	//
	// Keep it to the scale of contact detail — creases, the ground under an object.
	// Occlusion is measured on screen, and only what is on screen occludes, so a radius
	// on the scale of rooms shows its limits.
	Radius float32
	// Intensity deepens occlusion beyond what is measured, as a power of the share of
	// the sky a surface sees: 1 is the measure itself, 2 squares it — a point that sees
	// half its sky is lit by a quarter of the ambient light. 0 means 1.
	Intensity float32
	// FullResolution measures occlusion at every pixel rather than at one in four. It
	// costs about four times as much, for edges that hold their shape more closely.
	FullResolution bool
	// Thickness is how deep, in world units, AmbientOcclusionVBAO takes every surface to
	// be: something more than this behind a surface stays in view past it. Thinner shows
	// more behind pillars and posts, and less occlusion under large objects. 0 means the
	// radius.
	Thickness float32
	// Quality is how many samples occlusion takes, and how much it smooths them: higher
	// is less grainy, and costs more. 0 means AmbientOcclusionQualityMedium.
	Quality AmbientOcclusionQuality
}

// defaultAmbientOcclusion is what each zero field of an AmbientOcclusionSettings takes;
// a zero Thickness takes the radius.
var defaultAmbientOcclusion = AmbientOcclusionSettings{Radius: 0.5, Intensity: 1}

// resolved returns the settings with every zero field replaced by its default.
func (s AmbientOcclusionSettings) resolved() AmbientOcclusionSettings {
	if s.Radius == 0 {
		s.Radius = defaultAmbientOcclusion.Radius
	}
	if s.Intensity == 0 {
		s.Intensity = defaultAmbientOcclusion.Intensity
	}
	if s.Thickness == 0 {
		s.Thickness = s.Radius
	}
	if s.Quality < AmbientOcclusionQualityLow || s.Quality > AmbientOcclusionQualityHigh {
		s.Quality = AmbientOcclusionQualityMedium
	}
	return s
}

// topMip is the full-resolution mip occlusion is measured at: 0 at full resolution, 1
// at half.
func (s AmbientOcclusionSettings) topMip() uint32 {
	if s.FullResolution {
		return 0
	}
	return 1
}

// maxDepthChainMips is how many images the depth chain has, at most: XeGTAO's five.
const maxDepthChainMips = 5

// occlusionGroupSize is the side of the square workgroup every ambient occlusion compute
// pass runs (local_size_x and local_size_y in occlusion_*.comp.glsl).
const occlusionGroupSize = 8

// vbaoImages are the images VBAO measures occlusion in, all the size of mip topMip of
// the frame or smaller.
type vbaoImages struct {
	// depthChain is the scene's depth as a mip chain, whose first level is mip topMip of
	// the frame (see occlusion.glsl).
	depthChain textures.WritableTexture
	// occlusion is how open each surface is, and the edges between surfaces (see
	// vbao_slices.comp.glsl), the size of depthChain's first level; denoiseScratch
	// holds it between denoise passes.
	occlusion      gpu.Texture
	denoiseScratch gpu.Texture
	// upsampled is the occlusion brought up to full resolution, when it is measured at
	// less.
	upsampled gpu.Texture
	topMip    uint32
}

// openness is the image holding how open each surface is at full resolution.
func (images vbaoImages) openness() gpu.Texture {
	if images.upsampled.IsValid() {
		return images.upsampled
	}
	return images.occlusion
}

// isCreated reports whether the images exist.
func (images vbaoImages) isCreated() bool {
	return images.depthChain.IsValid()
}

// vbaoDepthRoot matches PC in vbao_depth.comp.glsl.
type vbaoDepthRoot struct {
	depthUnprojection glm.Vec4f
	depth             uint32
	depthSamples      uint32
	target            uint32
	topMip            uint32
	fullSize          [2]int32
	_                 [2]uint32
}

// vbaoDepthMipRoot matches PC in vbao_depth_mip.comp.glsl.
type vbaoDepthMipRoot struct {
	chain    uint32
	target   uint32
	mip      uint32
	topMip   uint32
	fullSize [2]int32
	_        [2]uint32
}

// vbaoSlicesRoot matches PC in vbao_slices.comp.glsl.
type vbaoSlicesRoot struct {
	pixelRay       glm.Vec4f
	pixelRayOrigin glm.Vec4f
	chain          uint32
	chainMips      uint32
	topMip         uint32
	target         uint32
	fullSize       [2]int32
	radius         float32
	thickness      float32
	slices         float32
	steps          float32
	_              [2]float32
}

// vbaoDenoiseRoot matches PC in vbao_denoise.comp.glsl.
type vbaoDenoiseRoot struct {
	source       uint32
	target       uint32
	size         [2]int32
	centerWeight float32
	_            [3]float32
}

// vbaoDenoiseCenterWeight is how much each texel counts for itself in the last of the
// denoise passes, and a fifth as much in the ones before, which smooth more (XeGTAO's
// DenoiseBlurBeta).
const vbaoDenoiseCenterWeight = 1.2

// vbaoUpsampleRoot matches PC in vbao_upsample.comp.glsl.
type vbaoUpsampleRoot struct {
	depthUnprojection glm.Vec4f
	depth             uint32
	depthSamples      uint32
	occlusion         uint32
	chain             uint32
	target            uint32
	topMip            uint32
	fullSize          [2]int32
}

// The ASSAO method (AmbientOcclusionASSAO) follows Intel's Adaptive SSAO,
// at its Low preset.
const (
	// assaoInterleavedImages is how many images the depth is split into: one for each
	// pixel of a 2x2 block of the images' grid.
	assaoInterleavedImages = 4
	// assaoFadeOutFrom and assaoFadeOutTo are the view depths, in world units, over
	// which occlusion fades out with distance.
	assaoFadeOutFrom = 50
	assaoFadeOutTo   = 300
)

// assaoImages are the images the ASSAO method measures occlusion in.
type assaoImages struct {
	// depth is the scene's depth split into interleaved images, each sampling every
	// stride pixels (see assao.glsl); occlusion and blurScratch are what is gathered in
	// each, and its blur's other half.
	depth       [assaoInterleavedImages]gpu.Texture
	occlusion   [assaoInterleavedImages]gpu.Texture
	blurScratch [assaoInterleavedImages]gpu.Texture
	stride      int32
}

// isCreated reports whether the images exist.
func (images assaoImages) isCreated() bool {
	return images.depth[0].IsValid()
}

// indices is the heap index of each of textures.
func indices(textures [assaoInterleavedImages]gpu.Texture) [assaoInterleavedImages]uint32 {
	var out [assaoInterleavedImages]uint32
	for i, t := range textures {
		out[i] = t.Index
	}
	return out
}

// assaoDepthRoot matches PC in assao_depth.comp.glsl.
type assaoDepthRoot struct {
	depthUnprojection glm.Vec4f
	depth             uint32
	depthSamples      uint32
	stride            int32
	targets           [assaoInterleavedImages]uint32
	interleavedSize   [2]int32
	fullSize          [2]int32
	_                 uint32
}

// assaoGatherRoot matches PC in assao_gather.comp.glsl.
type assaoGatherRoot struct {
	pixelRay           glm.Vec4f
	pixelRayOrigin     glm.Vec4f
	depth              [assaoInterleavedImages]uint32
	targets            [assaoInterleavedImages]uint32
	stride             int32
	radius             float32
	invRadiusNearLimit float32
	fadeOutMul         float32
	fadeOutAdd         float32
	interleavedSize    [2]int32
	fullSize           [2]int32
	taps               int32
	_                  [2]uint32
}

// assaoBlurRoot matches PC in assao_blur.comp.glsl.
type assaoBlurRoot struct {
	sources         [assaoInterleavedImages]uint32
	targets         [assaoInterleavedImages]uint32
	interleavedSize [2]int32
	_               [2]uint32
}

// occlusionApplyRoot matches PC in occlusion_apply.frag.glsl.
type occlusionApplyRoot struct {
	source     opennessSource
	outputKind occlusionOutput
}

// opennessSource matches OpennessSource in occlusion_openness.glsl: where a pass reads
// the openness occlusion measured, and how it shapes it.
type opennessSource struct {
	openness          uint32
	isInterleaved     uint32
	linearSampler     uint32
	stride            int32
	interleavedImages [assaoInterleavedImages]uint32
	interleavedSize   [2]int32
	intensity         float32
}

// occlusionOutput is what occlusion_apply.frag.glsl draws (OUTPUT_* there).
type occlusionOutput uint32

const (
	occlusionOutputOccludedShare occlusionOutput = iota
	occlusionOutputDarkening
	occlusionOutputOpenness
)

// occlusionDarkeningPass is the render pass the scene is darkened by its occlusion in.
type occlusionDarkeningPass uint8

const (
	// darkeningNotDrawn: ambient occlusion is off.
	darkeningNotDrawn occlusionDarkeningPass = iota
	// darkeningInOwnPass: a pass of its own, straight after the opaque scene.
	darkeningInOwnPass
	// darkeningInTransparentPass: first thing in the transparent pass.
	darkeningInTransparentPass
	// darkeningInToneMapping: by tone mapping, as it reads the scene, with no draws of
	// its own (see tonemap.frag.glsl).
	darkeningInToneMapping
)

// pixelRays is what viewPositionOnRay (occlusion.glsl) takes to place a pixel in view
// space, for a camera with projection seeing a frame width x height pixels: the view x
// and y its ray moves per unit of view z, and where the ray is at z = 0, each as
// (x's per-pixel factor, y's, x's constant, y's). It is viewPosition's arithmetic,
// with everything that does not depend on the pixel or its depth worked out here.
func pixelRays(projection glm.Mat4f, width, height uint32) (direction, origin glm.Vec4f) {
	// Column-major: [8] and [12] are x's z and constant terms, [9] and [13] y's, and
	// [11] and [15] w's.
	xScale, yScale := projection[0], projection[5]
	toNDCX, toNDCY := 2/float32(width), 2/float32(height)
	direction = glm.Vec4f{
		toNDCX * projection[11] / xScale,
		toNDCY * projection[11] / yScale,
		(-projection[11] - projection[8]) / xScale,
		(-projection[11] - projection[9]) / yScale,
	}
	origin = glm.Vec4f{
		toNDCX * projection[15] / xScale,
		toNDCY * projection[15] / yScale,
		(-projection[15] - projection[12]) / xScale,
		(-projection[15] - projection[13]) / yScale,
	}
	return direction, origin
}

// depthUnprojection is what turns a depth buffer's value back into view depth for a
// camera with projection: the rows of its inverse that give view z and w, as (z's depth
// factor, z's constant, w's depth factor, w's constant) — see linearViewDepth in
// occlusion.glsl.
func depthUnprojection(projection glm.Mat4f) glm.Vec4f {
	inverse := projection.Inv()
	return glm.Vec4f{inverse[10], inverse[14], inverse[11], inverse[15]}
}

// occlusionWorkgroupCount is how many workgroups an occlusion pass dispatches to cover size
// pixels along one axis.
func occlusionWorkgroupCount(size uint32) uint32 {
	return (size + occlusionGroupSize - 1) / occlusionGroupSize
}
