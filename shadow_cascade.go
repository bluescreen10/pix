// Cascaded shadow maps: split the view's depth range into a few slices and fit a
// separate shadow camera to each, so the slice nearest the viewer gets a whole map's
// worth of texels over a few units instead of a few hundred.
//
// This is the alternative to warping the map, which the perspective shadow map family
// does instead. A warp redistributes one map's texels, which buys a factor of a few and
// costs the texel grid, since the warp changes shape with the camera and leaves nothing
// to snap to. A cascade is just fitUniform over a short range, so snapping still works,
// quality near the viewer is set by the near slice's width rather than the scene's size,
// and nothing depends on the angle between the light and the view — which is what makes
// the dueling frustum a non-problem here rather than a case to detect and fall back on.
package pix

import (
	"github.com/bluescreen10/pix/glm"
	"github.com/chewxy/math32"
)

// MaxShadowCascades is the most slices a directional light can be split into. The lit
// shaders carry one matrix and one bias per cascade per light, so this bounds the light
// table rather than anything about the fit.
const MaxShadowCascades = 4

// DefaultShadowCascades is how many slices ShadowCascaded uses when nothing says
// otherwise.
const DefaultShadowCascades = 4

// defaultShadowNear is the fallback distance the split starts from when the camera's own
// near plane is uselessly close, expressed as a fraction of the covered range.
//
// A logarithmic split divides its range into equal ratios, so where it starts matters as
// much as where it ends: a near plane set a thousandth of the scene radius spends the
// first cascade on empty space and forces every later boundary outward to compensate.
// This is a poor substitute for knowing where geometry actually begins — it scales with
// the far distance, which the near field does not — so Renderer.SetShadowNear overrides
// it, and on any scene being tuned for quality it should.
const defaultShadowNear float32 = 1.0 / 500

// cascadeLevel is one fitted slice: the camera it renders with, the depth bias that fit
// implies, and the view distance it covers out to, which is what the lit shaders select
// on.
type cascadeLevel struct {
	cam Camera
	fit orthoFit
	far float32
}

// cascadeSplits fills out[i] with the view distance cascade i covers out to, the last
// being the whole slice's far distance.
//
// The split is logarithmic: each cascade covers the same RATIO of depth, which is what
// gives each of them comparable texels per screen pixel, since a screen pixel's world
// footprint also grows in proportion to distance. The textbook version blends in a
// uniform split to keep the logarithmic one off a too-close near plane; the caller's
// choice of near addresses that directly instead, and the blend does not survive a long
// range — over the 2000:1 a scene a few hundred units deep produces, even a twentieth of
// a uniform split contributes more than the entire logarithmic term and drags the first
// boundary out past the whole near field.
//
// That ratio is also the quality story: density falls by exactly it at every boundary,
// so covering [near, far] in n cascades costs a (far/near)^(1/n) cliff at each one.
func cascadeSplits(near, far float32, out []float32) {
	n := len(out)
	near = max(near, 1e-4)
	for i := range out {
		out[i] = near * math32.Pow(far/near, float32(i+1)/float32(n))
	}
	out[n-1] = far // exactly, so nothing falls past the last cascade
}

// sliceCorners re-cuts a fit's frustum corners to the range [near, far], measured along
// the corner rays from the eye. fit.corners are already capped at the shadow distance,
// so this only ever narrows them further.
func sliceCorners(fit shadowFit, near, far float32) [8]glm.Vec3f {
	span := fit.farDist - fit.nearDist
	if span <= 0 {
		return fit.corners
	}
	tNear := glm.Clamp((near-fit.nearDist)/span, 0, 1)
	tFar := glm.Clamp((far-fit.nearDist)/span, 0, 1)

	var out [8]glm.Vec3f
	for j := range 4 {
		edge := fit.corners[j+4].Sub(fit.corners[j])
		out[j] = fit.corners[j].Add(edge.Scale(tNear))
		out[j+4] = fit.corners[j].Add(edge.Scale(tFar))
	}
	return out
}
