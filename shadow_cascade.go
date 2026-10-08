// Cascaded shadow maps: split the view's depth range into a few slices and fit a
// separate shadow camera to each, so the slice nearest the viewer gets a whole map's
// worth of texels over a few units instead of a few hundred.
//
// This is the alternative to warping the map, which the perspective shadow map family
// does instead. A warp redistributes one map's texels, which buys a factor of a few and
// costs the texel grid, since the warp changes shape with the camera and leaves nothing
// to snap to. A cascade is an ordinary orthographic fit over a short range, so snapping still works,
// quality near the viewer is set by the near slice's width rather than the scene's size,
// and nothing depends on the angle between the light and the view — which is what makes
// the dueling frustum a non-problem here rather than a case to detect and fall back on.
package pix

import (
	"github.com/bluescreen10/pix/glm"
	"github.com/bluescreen10/pix/scenes"
)

// cascadeLevel is one fitted slice: the camera it renders with, the depth bias that fit
// implies, and the view depth it covers out to, which is what the lit shaders select
// on.
type cascadeLevel struct {
	cam Camera
	fit orthoFit
	far float32
}

// cascadeSplits fills out[i] with the view depth cascade i covers out to, the last
// being far exactly, so nothing falls past the last cascade. fractions are where each
// cascade but the last ends, as fractions of [near, far] (see
// scenes.DirectionalShadow.Splits).
//
// The splits are fixed fractions of a fixed distance, not derived from the scene. That
// keeps every slice the same depth from frame to frame, and a slice's bounding sphere
// then depends only on the camera's lens — not where it stands or looks — so the
// texel size holds still and snapping the center to the texel grid is all it takes to
// keep edges from crawling.
//
// The fractions are the caller's, so they are guarded rather than trusted: a split that
// does not advance would leave a slice no depth to fit.
func cascadeSplits(near, far float32, fractions [scenes.MaxShadowCascades - 1]float32, out []float32) {
	n := len(out)
	previous := near
	for i := range n - 1 {
		split := near + glm.Clamp(fractions[i], 0, 1)*(far-near)
		out[i] = max(split, previous*(1+1e-3))
		previous = out[i]
	}
	out[n-1] = far
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
