// Directional shadow-camera fitting. A directional light has no position to project
// from, so every frame the renderer has to choose a shadow camera that covers the part
// of the view worth shadowing. Which one it chooses is the shadow ALGORITHM, selected
// per renderer with Renderer.SetShadow.
//
// Both algorithms here produce the same kind of thing — a world→light-clip matrix the
// depth pass renders with and the lit shaders sample through (lighting.glsl's
// shadowFactor). Correctness never depends on which one is in use: shadow mapping works
// for any invertible projective matrix as long as the depth pass and the lookup share
// it, which they do. What differs is where the map's texels land, and so how much
// perspective aliasing (blocky shadow edges near the camera) is visible.
package pix

import (
	"math"

	"github.com/bluescreen10/pix/glm"
	"github.com/bluescreen10/pix/scenes"
	"github.com/chewxy/math32"
)

// ShadowSettings is what Renderer.SetShadows accepts: either ShadowUniform or
// ShadowCascaded. It selects how a directional light's shadow camera is fitted AND
// carries that choice's own configuration, so there is one place to set both rather than
// an algorithm switch beside a scattering of knobs that only apply to some of them.
//
// Spot and point lights are unaffected — their cameras follow from the light's own
// cone/position, so there is nothing to choose.
type ShadowSettings interface {
	// levels is how many shadow cameras a directional light needs.
	levels() int
}

// ShadowUniform fits one orthographic camera around the shadowed slice of the view
// frustum, snapped to the shadow map's texel grid. Texel density is constant across the
// whole map, which wastes resolution on distant geometry but is stable: the snapping
// means shadow edges do not crawl as the camera moves.
//
// It is the default, and the right choice when the shadowed range is short enough for
// one map. Over a large scene it cannot be: a single map stretched over a few hundred
// units puts well under a texel per screen pixel on the ground at the viewer's feet.
type ShadowUniform struct{}

func (ShadowUniform) levels() int {
	return 1
}

// ShadowCascaded splits the view's depth range into slices and fits a separate
// orthographic camera to each, sharing one map laid out side by side. See
// shadow_cascade.go.
//
// It is the only fit here whose quality near the viewer is set by how wide the nearest
// slice is rather than by how large the scene is, so it is the only one that holds up in
// a scene hundreds of units across. Because each slice is an ordinary uniform fit it
// keeps the texel grid, and so the stability a warped fit would have to give up; and
// because nothing about the split depends on where the light is, the dueling frustum is
// not a case it has to detect. It costs one depth pass per slice, and a map that many
// times wider.
type ShadowCascaded struct {
	// Levels is how many slices to split into, clamped to MaxShadowCascades. Zero means
	// DefaultShadowCascades. Ignored when Steps decides the count — see Steps.
	Levels int

	// Steps are the boundaries, innermost first: Steps[i] is the view distance slice i
	// covers out to, in world units from the eye, and the last is where shadows stop.
	// When it is used it also decides how many slices there are, so Levels is ignored;
	// that avoids the two disagreeing.
	//
	// Setting these explicitly is usually how a scene gets tuned, because the derived
	// ones cannot know where the geometry that matters is. What to look at is the RATIO
	// between consecutive steps: texel density drops by exactly that at each boundary,
	// so a 2x step is a mild change and a 20x step makes anything thin — a cable, a
	// railing — disappear outright as it crosses.
	Steps []float32

	// AutoSteps derives the boundaries from the view even when Steps is set, so a tuned
	// set can be kept around and compared against without being deleted. Steps is also
	// derived when it is empty, which is what makes the zero value work.
	AutoSteps bool
}

func (c ShadowCascaded) levels() int {
	if n := len(c.steps()); n > 0 {
		return n
	}
	if c.Levels <= 0 {
		return DefaultShadowCascades
	}
	return min(c.Levels, MaxShadowCascades)
}

// steps is the explicit boundary list if there is one to use, else nil.
func (c ShadowCascaded) steps() []float32 {
	if c.AutoSteps || len(c.Steps) == 0 {
		return nil
	}
	return c.Steps[:min(len(c.Steps), MaxShadowCascades)]
}

// Shadows reports how directional shadow cameras are fitted, and that fit's settings.
func (r *Renderer) Shadows() ShadowSettings {
	if r.shadows == nil {
		return ShadowUniform{}
	}
	return r.shadows
}

// SetShadows selects how directional shadow cameras are fitted — see ShadowSettings.
// It takes effect on the next Render: the fit is recomputed every frame, and a shadow
// map is reallocated only if the number of slices changed.
//
// This is not Renderer.EnableShadows, which turns shadow rendering on and off; this
// chooses how it is done when it is on.
func (r *Renderer) SetShadows(s ShadowSettings) {
	if s == nil {
		s = ShadowUniform{}
	}
	r.shadows = s
}

// shadowFit is what every directional fit needs to know about this frame: the view it
// is covering and the scene the casters live in. Gathered once per frame by
// prepareShadows, since every casting light shares it.
//
// corners is the view frustum already CAPPED at the shadow distance (see
// Renderer.shadowFitFor): near corners 0..3, then the far corners pulled back along
// their own edges. nearDist/farDist are that capped slice's distances from the eye,
// which is what the cascade split is computed over.
type shadowFit struct {
	eye     glm.Vec3f
	forward glm.Vec3f
	corners [8]glm.Vec3f

	nearDist float32
	farDist  float32

	sceneCenter glm.Vec3f
	sceneRadius float32
}

// shadowFitFor caps the view frustum at the shadow distance and gathers everything the
// fit algorithms share.
//
// The cap is what keeps a shadow map useful: fitted to the whole frustum, a far plane
// kilometres out would spread every texel across the horizon. The default caps at the
// last of the scene the camera can actually see — how far along the view direction the
// scene bounds reach, never past the camera's own far plane — so the slice covers
// everything that can receive a shadow and nothing beyond it. Override with
// Renderer.SetShadowDistance.
//
// distance is how far the slice should reach, in world units from the eye; zero derives
// it. A cascade split with explicit steps passes its last step, since that IS where its
// shadows stop — left to derive, a shorter auto distance would quietly clamp every step
// past it and collapse those cascades onto the same sliver of frustum.
//
// Covering exactly what is visible matters more than it sounds. fitUniform fits a
// bounding SPHERE around this slice, and a sphere around a frustum wedge reaches well
// past it, so a short cap there is invisible — the slop covers it. A cascade fits a much
// shorter slice with far less slop, so the same short cap shows up as a hard band of
// missing shadow at its far edge. A cap that is right to begin with avoids both.
func (r *Renderer) shadowFitFor(cam Camera, sceneCenter glm.Vec3f, sceneRadius float32, distance float32) shadowFit {
	corners := frustumCornersWorld(cam.ViewProjection())
	eye := cam.Position()

	nearCenter := avgCorners(corners, 0)
	farCenter := avgCorners(corners, 4)
	nearDist := nearCenter.Sub(eye).Length()
	farDist := farCenter.Sub(eye).Length()

	shadowDist := distance
	if shadowDist <= 0 {
		forward := farCenter.Sub(nearCenter).Normalize()
		// How far along the view the scene still reaches. A small floor keeps the slice
		// from collapsing when the camera sits on top of the scene, or has it behind.
		reach := sceneCenter.Sub(eye).Dot(forward) + sceneRadius
		shadowDist = min(max(reach, sceneRadius*0.15), farDist)
	}

	t := float32(1)
	if farDist > nearDist {
		t = glm.Clamp((shadowDist-nearDist)/(farDist-nearDist), 0, 1)
	}

	fit := shadowFit{
		eye:         eye,
		forward:     farCenter.Sub(nearCenter).Normalize(),
		nearDist:    nearDist,
		farDist:     nearDist + t*(farDist-nearDist),
		sceneCenter: sceneCenter,
		sceneRadius: sceneRadius,
	}
	for j := range 4 {
		fit.corners[j] = corners[j]
		fit.corners[j+4] = corners[j].Add(corners[j+4].Sub(corners[j]).Scale(t))
	}
	return fit
}

// fitDirectional points a directional light's shadow camera at the view, using the
// renderer's selected algorithm.
func (r *Renderer) fitDirectional(s *shadowResource, l scenes.LightPacket, fit shadowFit) {
	if c, ok := r.Shadows().(ShadowCascaded); ok {
		r.fitCascaded(s, l, c, fit)
		return
	}
	s.cascades = s.cascades[:0] // a previous frame's slices are not this fit's
	r.fitUniform(s, l, fit)
}

// fitUniform aims an orthographic camera to cover the capped view slice, sized to
// enclose it. When that slice is as large as the whole scene (zoomed out) it falls back
// to the scene sphere, so it is never worse than a whole-scene fit; zoomed in, it packs
// resolution into the near view. The camera is pulled back along -dir across the scene
// so occluders between the light and the slice are still captured, and the center is
// snapped to the shadow texel grid so edges don't crawl as the camera moves.
func (r *Renderer) fitUniform(s *shadowResource, l scenes.LightPacket, fit shadowFit) {
	s.fit = r.fitOrtho(s.orthoCamera(), l, fit, s.width)
	s.ndcBias = s.fit.bias
}

// fitOrtho is fitUniform's body, aimed at a camera the caller owns, returning the depth
// bias its choice of box implies. Cascades need this: each slice is an ordinary uniform
// fit, differing only in which camera it writes and how short a range it covers.
//
// size is the resolution of the map REGION this camera renders into, which for a cascade
// is one square of the atlas rather than the whole texture — the texel grid the fit
// snaps to has to be the one it will actually be sampled through.
func (r *Renderer) fitOrtho(cam Camera, l scenes.LightPacket, fit shadowFit, size uint32) orthoFit {
	f, ok := cam.(interface {
		SetPosition(glm.Vec3f)
		SetTarget(glm.Vec3f)
		SetUp(glm.Vec3f)
		SetFrustum(l, r, b, t float32)
		SetClip(near, far float32)
	})
	if !ok {
		return orthoFit{}
	}
	d := l.Direction.Normalize()

	// Bounding sphere of the capped slice; fall back to the scene sphere when the fit
	// isn't tighter (e.g. zoomed out), so we never do worse than whole-scene.
	center, radius := boundingSphere(fit.corners)
	if radius >= fit.sceneRadius {
		center, radius = fit.sceneCenter, fit.sceneRadius
	}

	// Quantize the radius so the texel size only changes in discrete steps as the camera
	// zooms. A continuously-resizing box would keep moving the texel grid under the
	// geometry, which is what makes edges crawl — snapping the center only helps while
	// the texel size holds still.
	//
	// The step has to be RELATIVE to the radius, not an absolute fraction of the scene.
	// An absolute step is a fixed number of world units, so it is invisible on a slice
	// far larger than one step and ruinous on a slice smaller than one: in a scene a few
	// hundred units across it rounded a three-unit near slice up to fourteen, throwing
	// away four fifths of the resolution exactly where the fit was trying to concentrate
	// it. Rounding up on a geometric grid costs the same proportion at every scale, which
	// is what makes it safe to fit a small slice at all. Quarter-octave steps waste at
	// most a fifth of the resolution, against the factor of two a whole-octave grid costs
	// on a large one.
	if radius > 0 {
		const stepsPerOctave = 4
		octave := math.Ceil(math.Log2(float64(radius))*stepsPerOctave) / stepsPerOctave
		radius = float32(math.Exp2(octave))
	}
	right, up := lightBasis(d)

	// Snap the center to the shadow texel grid in the light's right/up plane. This fit
	// is always square, so either axis names the same texel.
	texel := 2 * radius / float32(size)
	if texel > 0 {
		cx := snap(center.Dot(right), texel)
		cy := snap(center.Dot(up), texel)
		cz := center.Dot(d)
		center = right.Scale(cx).Add(up.Scale(cy)).Add(d.Scale(cz))
	}

	const near, farScale = float32(0.01), float32(4)
	far := fit.sceneRadius * farScale
	f.SetUp(up)
	f.SetPosition(center.Sub(d.Scale(fit.sceneRadius * 2))) // back up across the scene
	f.SetTarget(center)
	f.SetFrustum(-radius, radius, -radius, radius)
	f.SetClip(near, far) // depth spans the whole scene toward the light

	// The renderer decides where the camera goes; the caller owns the derived bias.
	return orthoBias(radius, far-near, size, l.ShadowBias)
}

// lightBasis builds a stable right/up pair perpendicular to the light direction,
// avoiding the degenerate LookAt when the light points along world up.
func lightBasis(d glm.Vec3f) (right, up glm.Vec3f) {
	up = glm.Vec3f{0, 1, 0}
	if math32.Abs(d.Dot(up)) > 0.99 {
		up = glm.Vec3f{0, 0, 1}
	}
	right = up.Cross(d).Normalize()
	return right, d.Cross(right).Normalize()
}

// avgCorners averages the 4 frustum corners starting at base (0 = near, 4 = far).
func avgCorners(c [8]glm.Vec3f, base int) glm.Vec3f {
	sum := c[base].Add(c[base+1]).Add(c[base+2]).Add(c[base+3])
	return sum.Scale(0.25)
}

// boundingSphere returns a center + radius enclosing the 8 points (centroid + max
// distance; not minimal, but stable under rotation, which matters for shadow shimmer).
func boundingSphere(p [8]glm.Vec3f) (glm.Vec3f, float32) {
	var center glm.Vec3f
	for _, v := range p {
		center = center.Add(v)
	}
	center = center.Scale(1.0 / 8.0)
	var radius float32
	for _, v := range p {
		d := v.Sub(center)
		if dsq := d.Dot(d); dsq > radius {
			radius = dsq
		}
	}
	return center, math32.Sqrt(radius)
}

// snap rounds x down to the nearest multiple of step (for texel-grid alignment).
func snap(x, step float32) float32 {
	return float32(math.Floor(float64(x/step))) * step
}

// ShadowFilter selects how wide a kernel a shadow lookup uses. Set it with
// Renderer.SetShadowFilter; it applies to every directional light the renderer draws.
// Spot and point lights always use ShadowFilterHard.
type ShadowFilter uint8

const (
	// ShadowFilterHard takes a single hardware PCF tap. The comparison and the bilinear
	// blend of the four texels it touches both happen in the texture unit, so even this
	// is a 2x2 kernel — but only 2x2, which leaves shadow edges stepping from texel to
	// texel wherever the map is coarser than the screen. One sample per light.
	ShadowFilterHard ShadowFilter = iota

	// ShadowFilterSoft approximates a 5x5 Gaussian in nine taps, by spacing the taps so
	// the 2x2 footprints they already blend tile the kernel instead of overlapping (see
	// shadowSoft in lighting.glsl). Nine samples per light, for a 6x6 texel footprint.
	//
	// It is the better default for a cascade: the slices nearest the viewer resolve
	// finely enough that a 2x2 kernel leaves visible stair-stepping, and widening the
	// filter is what turns that into an edge.
	ShadowFilterSoft
)

// shadowFilterNames is the console/round-trip spelling of each filter, in enum order.
var shadowFilterNames = [...]string{"hard", "soft"}

// String returns the filter's name ("hard", "soft").
func (f ShadowFilter) String() string {
	if int(f) < len(shadowFilterNames) {
		return shadowFilterNames[f]
	}
	return "hard"
}

// ParseShadowFilter resolves a filter by name. The second result reports whether the
// name was known.
func ParseShadowFilter(s string) (ShadowFilter, bool) {
	for i, name := range shadowFilterNames {
		if name == s {
			return ShadowFilter(i), true
		}
	}
	return ShadowFilterHard, false
}

// ShadowFilterNames lists every accepted name, for help text and completion.
func ShadowFilterNames() []string {
	return shadowFilterNames[:]
}

// ShadowFilter reports which kernel directional shadow lookups use.
func (r *Renderer) ShadowFilter() ShadowFilter {
	return r.shadowFilter
}

// SetShadowFilter selects the kernel directional shadow lookups use — see ShadowFilter.
// It takes effect on the next Render; nothing needs reallocating.
func (r *Renderer) SetShadowFilter(filter ShadowFilter) {
	r.shadowFilter = filter
}
