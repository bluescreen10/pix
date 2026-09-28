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

// shadowFit is what every directional fit needs to know about this frame: the view it
// is covering and the scene the casters live in. Gathered once per frame by
// fitShadows, since every casting light shares it.
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
