// Scene-wide distance fog: the models a user picks from (LinearFog, Exp2Fog) and the
// packed form the light table carries to the shaders. The fog factor is evaluated
// per-fragment in the lit shaders — see applyFog in lighting.glsl.
package pix

import "github.com/bluescreen10/pix/colors"

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
	color   colors.RGB32F
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
	Color colors.RGB32F
	Near  float32
	Far   float32
}

// NewLinearFog returns a linear fog of colour c between near and far world units.
func NewLinearFog(color colors.RGB32F, near, far float32) *LinearFog {
	return &LinearFog{Color: color, Near: near, Far: far}
}

func (f *LinearFog) fogState() fogState {
	return fogState{color: f.Color, mode: fogLinear, near: f.Near, far: f.Far}
}

// exp2VisibleAtDistance is the fraction of a surface still showing through Exp2Fog at
// its Distance — 10%, i.e. "essentially gone, but not mathematically gone". It fixes
// the constant that converts a distance into the density the shader wants:
// exp(-(d*density)^2) = 0.1  =>  density = sqrt(-ln(0.1)) / d.
const exp2VisibleAtDistance = 0.1

// exp2DensityScale is sqrt(-ln(exp2VisibleAtDistance)), precomputed.
const exp2DensityScale = 1.5174271

// Exp2Fog falls off as exp(-(d/Distance * k)^2) — three.js's THREE.FogExp2, but
// parameterized by a distance rather than a raw density.
//
// Distance is where the fog has closed in: a surface that far from the camera shows
// through at about 10%. Nearer geometry keeps its colour, and the falloff is squared
// rather than plain exponential, so the foreground stays clear instead of hazing from
// the camera outward.
//
// A distance is the knob rather than a density because density is an inverse length,
// so the usable value depends entirely on how big the scene is: an asset authored in
// centimetres needs a density three or four orders of magnitude smaller than one
// authored in metres, and a value that looks reasonable typed out will flatten a large
// scene to a single colour. A distance is in the same units as everything else you
// already work in. To port a three.js density, use Distance = 1.5174 / density.
//
// A Distance of zero or less disables the fog rather than dividing by it.
//
// The fields are exported and read every frame, so they can be animated in place.
type Exp2Fog struct {
	Color    colors.RGB32F
	Distance float32
}

// NewExp2Fog returns an exponential-squared fog of colour c that has closed in at the
// given distance, in world units.
func NewExp2Fog(color colors.RGB32F, distance float32) *Exp2Fog {
	return &Exp2Fog{Color: color, Distance: distance}
}

func (f *Exp2Fog) fogState() fogState {
	if f.Distance <= 0 {
		return fogState{mode: fogNone}
	}
	return fogState{color: f.Color, mode: fogExp2, density: exp2DensityScale / f.Distance}
}

// stateOf returns the packed state for a Fog, treating nil as "no fog" so callers
// don't each repeat the check.
func stateOf(f Fog) fogState {
	if f == nil {
		return fogState{mode: fogNone}
	}
	return f.fogState()
}
