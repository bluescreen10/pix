// Scene-wide distance fog: the models a user picks from (LinearFog, Exp2Fog) and the
// packed form the light table carries to the shaders. The fog factor is evaluated
// per-fragment in the lit shaders — see applyFog in lighting.glsl.
package pix

import "github.com/bluescreen10/pix/glm"

// Fog modes, mirroring FOG_* in lighting.glsl. The mode travels in the alpha channel
// of the packed colour, so "no fog" costs no extra field.
const (
	fogNone uint32 = iota
	fogLinear
	fogExp2
)

// Fog is a scene-wide distance fog model. Implementations are LinearFog (a linear
// ramp between two distances) and Exp2Fog (exponential-squared falloff); a nil Fog —
// the default — disables fogging entirely.
//
// Fog is applied to lit surfaces in linear space, before the sRGB encode, so it
// blends the shaded colour toward the fog colour rather than washing it out. Set the
// fog colour to match the background and distant geometry dissolves into the horizon,
// which is the usual reason to reach for this: it hides the far plane.
type Fog interface {
	// fogState packs the model into the form the light table carries. Unexported, so
	// Fog is a closed set — the shader has to understand every mode.
	fogState() fogState
}

// fogState is the packed, shader-facing form of a Fog. Values are in world units.
type fogState struct {
	color   glm.Vec3f
	mode    uint32
	near    float32
	far     float32
	density float32
}

// LinearFog ramps linearly from no fog at Near to full fog at Far, and is the
// equivalent of three.js's THREE.Fog. It is not physically derived, which is exactly
// why artists like it: the two distances say precisely where the effect starts and
// where geometry has vanished.
//
// The fields are exported and read every frame, so they can be animated in place.
type LinearFog struct {
	Color glm.Vec3f
	Near  float32
	Far   float32
}

// NewLinearFog returns a linear fog of colour c between near and far world units.
func NewLinearFog(c glm.Vec3f, near, far float32) *LinearFog {
	return &LinearFog{Color: c, Near: near, Far: far}
}

func (f *LinearFog) fogState() fogState {
	return fogState{color: f.Color, mode: fogLinear, near: f.Near, far: f.Far}
}

// Exp2Fog falls off as exp(-(distance*Density)^2), the equivalent of three.js's
// THREE.FogExp2. Squaring the exponent is what makes this the usual default over a
// plain exponential: it leaves the foreground clear instead of hazing from the camera
// outward, then closes in quickly. Density is small — 0.01 fades things out over
// roughly a hundred world units.
//
// The fields are exported and read every frame, so they can be animated in place.
type Exp2Fog struct {
	Color   glm.Vec3f
	Density float32
}

// NewExp2Fog returns an exponential-squared fog of colour c and the given density.
func NewExp2Fog(c glm.Vec3f, density float32) *Exp2Fog {
	return &Exp2Fog{Color: c, Density: density}
}

func (f *Exp2Fog) fogState() fogState {
	return fogState{color: f.Color, mode: fogExp2, density: f.Density}
}

// stateOf returns the packed state for a Fog, treating nil as "no fog" so callers
// don't each repeat the check.
func stateOf(f Fog) fogState {
	if f == nil {
		return fogState{mode: fogNone}
	}
	return f.fogState()
}
