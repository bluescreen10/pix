// Environment lighting: what the renderer derives from a scene's scenes.Environment —
// its reflections, prefiltered by roughness, and its diffuse light, as spherical
// harmonics — and the BRDF table every environment's reflections are weighted by.
package pix

import (
	"github.com/bluescreen10/gamekit/gpu"
	"github.com/bluescreen10/pix/glm"
	"github.com/bluescreen10/pix/textures"
)

// The prefiltered reflections are a cube of faces environmentRadianceSize texels square
// with environmentMips levels, one per roughness from 0 to 1: 64, halving down to 2 for
// the roughest, which blurs far beyond needing more. Four faces of 64 go around the
// horizon in 256 texels, as many as the equirectangular image it replaced, and a lookup
// along a direction needs no trigonometry to find its texel.
const (
	environmentRadianceSize = 64
	environmentMips         = 6
)

// environmentMipBlurs is the GGX roughness each mip of the prefiltered reflections
// blurs the mip before it by, so that, compounded with the blurs before it, mip k holds
// the reflections of roughness k/(environmentMips-1). Blurs do not add up like their
// roughnesses: blurring each mip at its own roughness, as an earlier version did, left
// the roughest mip with the step between a red and a blue half of the sky at 0.71 red
// where one blur of roughness 1 gives 0.89. These were fitted by simulating the chain for that step at a normal 0.78 up,
// and hold to 0.02 at 0.3 up. Refit them if environmentMips changes.
var environmentMipBlurs = [environmentMips]float32{0, 0.20, 0.38, 0.44, 0.46, 0.41}

// environmentBRDFSize is the side of the BRDF table, ENV_BRDF_SIZE in environment.glsl.
const environmentBRDFSize = 64

// noEnvironment is the envRadiance sentinel: a scene lit by its flat ambient colour.
const noEnvironment uint32 = 0xFFFFFFFF

// environmentState is one scene's environment light, derived from its image: the
// prefiltered reflections, with a storage view of each of their mips for the pass that
// writes it, and the diffuse light, as spherical-harmonic coefficients. The renderer
// creates them on the scene's behalf while it has an environment, and frees them when
// it no longer does (see Renderer.prepareEnvironment); no resources means no
// environment.
type environmentState struct {
	radiance textures.WritableTexture
	// irradiance holds environmentIrradianceSize bytes: nine RGB coefficients, written
	// by env_irradiance.comp.glsl and read through the light table.
	irradiance gpu.Buffer
	// revision is the environment revision it was derived from; 0 until it has been.
	revision uint64
}

// environmentIrradianceSize is the size of the diffuse light's coefficients: nine
// float32 RGB triples.
const environmentIrradianceSize = 9 * 3 * 4

// destroy frees what the state holds.
func (e *environmentState) destroy(backend gpu.Backend) {
	e.radiance.Release()
	if e.irradiance.IsValid() {
		backend.Free(e.irradiance)
	}
	*e = environmentState{}
}

// envPrefilterRoot matches PC in env_prefilter.comp.glsl.
type envPrefilterRoot struct {
	source, target, sampler uint32
	blurRoughness           float32
	size                    uint32
	sourceIsCube            uint32
	_                       [2]uint32
}

// envIrradianceRoot matches PC in env_irradiance.comp.glsl.
type envIrradianceRoot struct {
	irradiance uint64
	radiance   uint32
	sampler    uint32
	size       uint32
	_          uint32
}

// envBRDFRoot matches PC in env_brdf.comp.glsl.
type envBRDFRoot struct {
	table uint32
	_     [3]uint32
}

// envBackgroundParams follows POSTFX_ROOT in env_background.frag.glsl.
type envBackgroundParams struct {
	inverseViewProj     glm.Mat4f
	environment         uint32
	intensity, rotation float32
	_                   uint32
}
