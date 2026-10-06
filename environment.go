// Environment lighting: what the renderer derives from a scene's scenes.Environment —
// its reflections, prefiltered by roughness, the roughest of which is also its diffuse
// light — and the BRDF table every environment's reflections are weighted by.
package pix

import (
	"github.com/bluescreen10/gamekit/gpu"
	"github.com/bluescreen10/pix/glm"
)

// The prefiltered reflections are an equirectangular image of
// environmentRadianceWidth x environmentRadianceHeight with environmentMips levels, one
// per roughness from 0 to 1: 256 x 128, halving down to 8 x 4 for the roughest, which
// blurs far beyond needing more.
const (
	environmentRadianceWidth  = 256
	environmentRadianceHeight = 128
	environmentMips           = 6
)

// environmentMipBlurs is the GGX roughness each mip of the prefiltered reflections
// blurs the mip before it by, so that, compounded with the blurs before it, mip k holds
// the reflections of roughness k/(environmentMips-1). Blurs do not add up like their
// roughnesses: blurring each mip at its own roughness, as an earlier version did, left
// the roughest mip — the diffuse light — with the step between a red and a blue half of
// the sky at 0.71 red where one blur of roughness 1 gives 0.89, the same as a cosine
// lobe. These were fitted by simulating the chain for that step at a normal 0.78 up,
// and hold to 0.02 at 0.3 up. Refit them if environmentMips changes.
var environmentMipBlurs = [environmentMips]float32{0, 0.20, 0.38, 0.44, 0.46, 0.41}

// environmentBRDFSize is the side of the BRDF table, ENV_BRDF_SIZE in environment.glsl.
const environmentBRDFSize = 64

// noEnvironment is the envRadiance sentinel: a scene lit by its flat ambient colour.
const noEnvironment uint32 = 0xFFFFFFFF

// environmentState is one scene's environment light, derived from its image: the
// prefiltered reflections, and a storage view of each of their mips for the pass that
// writes it. The renderer
// creates them on the scene's behalf while it has an environment, and frees them when
// it no longer does (see Renderer.prepareEnvironment); no resources means no
// environment.
type environmentState struct {
	radiance gpu.Texture
	mipViews [environmentMips]gpu.Texture
	// revision is the environment revision it was derived from; 0 until it has been.
	revision uint64
}

// destroy frees what the state holds.
func (e *environmentState) destroy(backend gpu.Backend) {
	for _, view := range e.mipViews {
		if view.IsValid() {
			backend.DestroyTexture(view)
		}
	}
	if e.radiance.IsValid() {
		backend.DestroyTexture(e.radiance)
	}
	*e = environmentState{}
}

// envPrefilterRoot matches PC in env_prefilter.comp.glsl.
type envPrefilterRoot struct {
	source, target, sampler uint32
	blurRoughness           float32
	size                    [2]uint32
	_                       [2]uint32
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
