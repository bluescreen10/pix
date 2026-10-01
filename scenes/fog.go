// Scene-wide distance fog: the models a user picks from (LinearFog, Exp2Fog) and the
// packed form the light table carries to the shaders. The fog factor is evaluated
// per-fragment in the lit shaders — see applyFog in lighting.glsl.
package scenes

import "github.com/bluescreen10/pix/colors"

// Fog modes, mirroring FOG_* in lighting.glsl. The mode travels in the alpha channel
// of the packed colour, so "no fog" costs no extra field. Exported because they are
// what FogState.Mode holds, and a consumer packing the light table has to name them.
const (
	FogNone uint32 = iota
	FogLinear
	FogExp2
	FogVolumetric
)

// Fog is a scene-wide fog model. Implementations are LinearFog (a linear ramp between
// two distances), Exp2Fog (exponential-squared falloff) and VolumetricFog (a lit medium
// the renderer simulates); a nil Fog — the default — disables fogging entirely.
//
// Fog is applied to lit surfaces in linear space, before the sRGB encode, so it
// blends the shaded colour toward the fog colour rather than washing it out. Set the
// fog colour to match the background and distant geometry dissolves into the horizon,
// which is the usual reason to reach for this: it hides the far plane.
type Fog interface {
	// fogState resolves the model to the values a packet carries. Unexported, so Fog
	// stays a closed set — the shader has to understand every mode.
	fogState() FogState
}

// FogState is the resolved, shader-facing form of a Fog: the values a packet carries.
// The packet holds this rather than the Fog interface, because a frame description
// should carry values a consumer can read, not an interface it has to call back into.
type FogState struct {
	Color   colors.RGB32F
	Mode    uint32
	Near    float32
	Far     float32
	Density float32

	// The rest describe a volumetric fog (see VolumetricFog); the other models leave
	// them zero. Density above is its density at BaseHeight, and HeightFalloff how much
	// it thins per unit of height above that: the inverses of the Visibility and
	// Thickness it was given, which is what the shaders work in.
	Albedo        colors.RGB32F
	Emission      colors.RGB32F
	Anisotropy    float32
	BaseHeight    float32
	HeightFalloff float32
	Reach         float32
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

func (f *LinearFog) fogState() FogState {
	return FogState{Color: f.Color, Mode: FogLinear, Near: f.Near, Far: f.Far}
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

func (f *Exp2Fog) fogState() FogState {
	if f.Distance <= 0 {
		return FogState{Mode: FogNone}
	}
	return FogState{Color: f.Color, Mode: FogExp2, Density: exp2DensityScale / f.Distance}
}

// VolumetricFog is fog the renderer simulates as a medium filling the space in front of
// the camera: lights scatter through it, so a lamp shows a glow and shadows cut shafts
// through it, and every surface, opaque or blended, is fogged by what lies between it
// and the camera. It costs two compute passes a frame; how finely they sample it is the
// renderer's to choose (see pix.Renderer.SetVolumetricFog).
//
// Its extents are distances rather than densities, for the reason Exp2Fog gives: a
// density is an inverse length, so a value that reads sensibly in one scene's units
// erases another scene or does nothing in it. A distance is in the units you already
// build the scene in.
//
// The fields are exported and read every frame, so they can be animated in place.
type VolumetricFog struct {
	// Visibility is how far you can see through the fog at BaseHeight and below: a
	// surface that far away still shows 37% (1/e) of its light. Zero or less disables
	// the fog.
	Visibility float32
	// BaseHeight is where the fog starts to thin, and Thickness how quickly: every
	// Thickness units above BaseHeight it is a third (1/e) as dense, so it pools in low
	// ground. A Thickness of zero keeps the fog the same at every height.
	BaseHeight float32
	Thickness  float32
	// Reach is how far from the camera the fog is simulated. A surface beyond it is
	// fogged as if it were at Reach. Zero or less disables the fog.
	Reach float32

	// Albedo is the fraction of the light the fog takes out that it scatters rather
	// than absorbs, per channel: lit fog takes its colour, and black fog only darkens.
	// Emission is light the fog gives off on its own, per unit of density.
	Albedo   colors.RGB32F
	Emission colors.RGB32F

	// Anisotropy is how much of the light the fog scatters keeps going forward: 0
	// scatters it evenly, and toward 1 looking at a light shows a bright halo around
	// it. Negative values scatter it back toward the light.
	Anisotropy float32
}

// NewVolumetricFog returns white fog you can see visibility world units into,
// simulated out to reach world units from the camera, the same at every height.
func NewVolumetricFog(visibility, reach float32) *VolumetricFog {
	return &VolumetricFog{Visibility: visibility, Reach: reach, Albedo: colors.RGB32F{1, 1, 1}}
}

func (f *VolumetricFog) fogState() FogState {
	if f.Visibility <= 0 || f.Reach <= 0 {
		return FogState{Mode: FogNone}
	}
	var falloff float32
	if f.Thickness > 0 {
		falloff = 1 / f.Thickness
	}
	return FogState{
		Mode:          FogVolumetric,
		Density:       1 / f.Visibility,
		Albedo:        f.Albedo,
		Emission:      f.Emission,
		Anisotropy:    f.Anisotropy,
		BaseHeight:    f.BaseHeight,
		HeightFalloff: falloff,
		Reach:         f.Reach,
	}
}

// StateOf returns the resolved state for a Fog, treating nil as "no fog" so callers
// don't each repeat the check.
func StateOf(f Fog) FogState {
	if f == nil {
		return FogState{Mode: FogNone}
	}
	return f.fogState()
}
