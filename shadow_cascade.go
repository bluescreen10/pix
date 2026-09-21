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
	"github.com/bluescreen10/pix/scenes"
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

// ShadowNear reports the distance ShadowCascaded starts its split from, or 0 when it is
// derived from the view.
func (r *Renderer) ShadowNear() float32 {
	return r.shadowNear
}

// SetShadowNear sets the distance, in world units from the eye, that ShadowCascaded
// starts its cascade split from. Zero (the default) derives it from the covered range.
//
// Geometry nearer than this is still shadowed — the first cascade is fitted from the
// camera's real near plane, and only the BOUNDARIES are computed from this. What it
// controls is the ratio between consecutive cascades, and that ratio is the whole story
// for quality: each boundary drops texel density by exactly that factor, so a split
// starting far too close forces large ratios and a visible cliff at every boundary. Set
// it to roughly where the nearest geometry the camera can see begins.
func (r *Renderer) SetShadowNear(distance float32) {
	r.shadowNear = distance
}

// cascadeLevel is one fitted slice: the camera it renders with, the depth bias that fit
// implies, and the view distance it covers out to, which is what the lit shaders select
// on.
type cascadeLevel struct {
	cam Camera
	fit orthoFit
	far float32
}

// shadowReach is how far the shadow fit should cover, in world units from the eye, or
// zero to derive it from the view.
//
// Explicit cascade steps decide it: the outermost one is by definition where shadows
// stop, so it has to set the range rather than be clipped by a separately chosen one.
// Renderer.SetShadowDistance therefore has no effect while explicit steps are in use,
// which keeps one setting in charge of the far end instead of two disagreeing.
func (r *Renderer) shadowReach() float32 {
	if c, ok := r.Shadows().(ShadowCascaded); ok {
		if steps := c.steps(); len(steps) > 0 {
			return steps[len(steps)-1]
		}
	}
	return r.shadowDistance
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

// fitCascaded fits one orthographic camera per slice of the view. Each slice is the
// same frustum capped to a shorter range, so every cascade is an ordinary uniform fit —
// texel snapping and all — over a range short enough for its texels to matter.
func (r *Renderer) fitCascaded(s *shadowResource, l scenes.LightPacket, c ShadowCascaded, fit shadowFit) {
	count := c.levels()
	s.ensureCascades(count)

	var splits [MaxShadowCascades]float32
	if explicit := c.steps(); explicit != nil {
		copy(splits[:count], explicit)
	} else {
		splitNear := r.shadowNear
		if splitNear <= 0 {
			splitNear = fit.farDist * defaultShadowNear
		}
		cascadeSplits(max(splitNear, fit.nearDist), fit.farDist, splits[:count])
	}

	// The first cascade is fitted from the camera's real near plane, whatever the split
	// started from, so geometry in between is still covered.
	near := fit.nearDist
	for i := range count {
		// Explicit steps are the caller's, so they are guarded rather than trusted: a
		// step that does not advance would give a slice no depth to fit.
		far := max(splits[i], near*(1+1e-3))
		lvl := &s.cascades[i]
		lvl.far = far
		// The slice's own frustum: the same corner rays, cut at this cascade's near and
		// far rather than the whole range's.
		sub := fit
		sub.nearDist, sub.farDist = near, far
		sub.corners = sliceCorners(fit, near, far)
		lvl.fit = r.fitOrtho(lvl.cam, l, sub, s.size())
		near = far
	}
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
