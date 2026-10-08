// Directional shadow-camera fitting. A directional light has no position to project
// from, so every frame the renderer chooses shadow cameras that cover the part of the
// view the light's shadow settings ask for (see scenes.DirectionalShadow): the view out
// to its distance, cut into its cascades.
//
// Every camera here produces the same kind of thing — a world→light-clip matrix the
// depth pass renders with and the lit shaders sample through (lighting.glsl's
// shadowFactor). What the fit decides is where the map's texels land, and so how much
// perspective aliasing (blocky shadow edges near the camera) is visible.
package pix

import (
	"math"

	"github.com/bluescreen10/pix/glm"
	"github.com/chewxy/math32"
)

// shadowFit is what every directional fit needs to know about this frame: the view it
// is covering and the scene the casters live in. Gathered per light by fitShadows,
// since each light's shadow reaches its own distance.
//
// corners is the view frustum already CAPPED at the light's shadow distance (see
// Renderer.shadowFitFor): near corners 0..3, then the far corners pulled back along
// their own edges. nearDist/farDist are that capped slice's distances from the eye,
// which is what the cascade split is computed over.
type shadowFit struct {
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
