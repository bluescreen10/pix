// Light types: what a user creates and configures (DirectionalLight, PointLight,
// SpotLight) plus the per-light shadow resources. The flat GPU table these are
// compiled into each frame lives in scene_lights.go.
package scenes

import (
	"github.com/bluescreen10/pix/colors"
	"github.com/bluescreen10/pix/glm"
	"github.com/bluescreen10/pix/textures"
)

// LightShadow is a light's shadow SETTINGS — the resolution and bias a caller asks
// for. It deliberately holds no camera, no depth map and no fitted matrix: those are
// renderer-owned resources, cached per light identity and reachable through
// Renderer.ShadowView. See docs/frame-packet.md.
//
// The split is what lets a Scene be described without a renderer at all. Fitting a
// shadow camera depends on the view being rendered and on caster bounds the renderer
// tracks, so it was never something the light could own; before the split it only
// looked that way because renderer code reached in and wrote these fields.
// DefaultShadowSize is the resolution a shadow map is requested at when the caller
// does not choose one. It lives here rather than in a renderer because it is part of
// what the light asks for; a packet may still carry 0, which a consumer reads as "your
// default, whatever that is".
const DefaultShadowSize uint32 = 1024

type LightShadow struct {
	size uint32  // requested resolution per side
	bias float32 // extra depth offset, in WORLD units
}

func newLightShadow() *LightShadow {
	return &LightShadow{size: DefaultShadowSize}
}

// Size is the shadow map's requested resolution per side.
func (s *LightShadow) Size() uint32 {
	return s.size
}

// SetSize sets the shadow map's resolution per side. The renderer reallocates its map
// at the new resolution on the next frame that renders it; a zero size is ignored.
func (s *LightShadow) SetSize(size uint32) {
	if size == 0 {
		return
	}
	s.size = size
}

// Bias is the extra depth offset applied to the shadow comparison, in world units.
func (s *LightShadow) Bias() float32 {
	return s.bias
}

// SetBias sets an extra depth offset for the shadow comparison, in WORLD units, on top
// of a bias the renderer derives from the map's texel footprint. 0 (the default) is
// usually right — the derived term already scales with the fit, so it works at any
// scene scale. Raise this if surfaces self-shadow (acne).
func (s *LightShadow) SetBias(bias float32) {
	s.bias = bias
}

// MaxShadowCascades is the most slices a directional light's shadow can be split into.
// A renderer carries one matrix per cascade per light, so this bounds what it has to
// reserve rather than anything about the fit.
const MaxShadowCascades = 4

// ShadowMethod is how a directional light's shadow map is laid over the part of the
// view it covers.
type ShadowMethod uint8

const (
	// ShadowUniform fits an orthographic camera around each cascade's slice of the
	// view, snapped to the map's texel grid. Texel density is constant across a
	// cascade, which wastes resolution on its far end but is stable: the snapping means
	// shadow edges do not crawl as the camera moves. The default.
	ShadowUniform ShadowMethod = iota
)

// DefaultShadowCascades, DefaultShadowDistance and DefaultShadowSplits are what a
// directional light's shadow starts with: two cascades over the first 100 units of the
// view, the first ending at a tenth of it. The later splits, at a fifth and at half,
// take effect when more cascades are asked for.
const (
	DefaultShadowCascades         = 2
	DefaultShadowDistance float32 = 100
)

var DefaultShadowSplits = [MaxShadowCascades - 1]float32{0.1, 0.2, 0.5}

// DirectionalShadow is a directional light's shadow settings: the resolution and bias
// every light has, plus how its shadow covers the view. A directional light has no
// position to project from, so a renderer fits a shadow camera to the view every frame;
// these say how far that fit reaches and how it is sliced.
//
// The view out to Distance is split into Cascades slices, each fitted with a map of its
// own, so the slice nearest the viewer gets a whole map over a few units instead of
// sharing one with the horizon. A single cascade is one map over the whole distance.
type DirectionalShadow struct {
	LightShadow
	method   ShadowMethod
	cascades int
	distance float32
	splits   [MaxShadowCascades - 1]float32
}

func newDirectionalShadow() *DirectionalShadow {
	return &DirectionalShadow{
		LightShadow: LightShadow{size: DefaultShadowSize},
		cascades:    DefaultShadowCascades,
		distance:    DefaultShadowDistance,
		splits:      DefaultShadowSplits,
	}
}

// Method is how the shadow map is laid over the view.
func (s *DirectionalShadow) Method() ShadowMethod {
	return s.method
}

// SetMethod sets how the shadow map is laid over the view; see ShadowMethod. A value
// that names no method sets ShadowUniform.
func (s *DirectionalShadow) SetMethod(method ShadowMethod) {
	if method > ShadowUniform {
		method = ShadowUniform
	}
	s.method = method
}

// Cascades is how many slices the view is split into.
func (s *DirectionalShadow) Cascades() int {
	return s.cascades
}

// SetCascades sets how many slices the view is split into, clamped to
// [1, MaxShadowCascades]. Each one costs a depth pass, and its map is a square of Size
// beside the others.
func (s *DirectionalShadow) SetCascades(count int) {
	s.cascades = min(max(count, 1), MaxShadowCascades)
}

// Distance is how far from the eye, along the view, the shadow reaches.
func (s *DirectionalShadow) Distance() float32 {
	return s.distance
}

// SetDistance sets how far from the eye, along the view, the shadow reaches, in world
// units; nothing beyond it is shadowed. A shorter distance gives every cascade a
// shorter slice and so sharper shadows. A renderer never reaches past the camera's own
// far plane. Zero or less is ignored.
func (s *DirectionalShadow) SetDistance(distance float32) {
	if distance <= 0 {
		return
	}
	s.distance = distance
}

// Splits are where each cascade but the last ends, as fractions of the distance,
// innermost first. Only the first Cascades()-1 are used: two cascades split at the
// first, four at all three.
func (s *DirectionalShadow) Splits() [MaxShadowCascades - 1]float32 {
	return s.splits
}

// SetSplits sets where each cascade but the last ends, as fractions of the distance,
// innermost first; see Splits.
//
// Texel density drops at each split by roughly the ratio between the slices either side
// of it, so the first split is the one that matters most: it decides how much of the
// view gets the sharpest map.
func (s *DirectionalShadow) SetSplits(splits [MaxShadowCascades - 1]float32) {
	s.splits = splits
}

// DirectionalLight is a distant light with parallel rays (a sun). Direction is the
// direction the light travels (e.g. {0,-1,0} for a downward sun). Fields are exported
// and may be changed at any time; the scene re-derives the GPU light table each frame.
type DirectionalLight struct {
	Direction glm.Vec3f
	Color     colors.RGB32F
	Intensity float32
	shadow    *DirectionalShadow
	mask      LightMask
	id        LightID
}

// LightMask is a texture laid over the world across a directional light, scaling how
// much of the light gets through: the shadow of clouds far above. Shadow maps are the
// wrong tool for that — fitted to the view, sharp, and only as far as the shadow
// distance — where a mask is soft, fixed in the world, and covers everything.
//
// Its red channel is the fraction of the light let through: 1 fully lit, 0 none. A
// point reads the texel its ray of light crosses, so the mask shades as if cast from
// above everything in the scene.
type LightMask struct {
	// Texture is the mask. The light holds a reference of its own while it is set.
	Texture textures.Texture
	// Size is how many world units one repeat of the texture spans; it repeats beyond.
	// For a light travelling straight down, u runs along world x and v along world z,
	// like a map; for a slanted one the two axes tilt with it. Zero or less disables
	// the mask.
	Size float32
	// Offset moves the mask through the world, in world units: animate it for wind.
	Offset glm.Vec3f
}

// LightMaskAxes returns the world directions a directional light's mask runs along, for
// a light shining along direction: u and v, square to the light and to each other. A
// world position p reads the mask at (u·(p - Offset), v·(p - Offset)) / Size, so every
// point on one of the light's rays reads the same texel. For a light shining straight
// down u is world x and v world z; a slanted light tilts them with it, and one shining
// along x takes z for u instead.
func LightMaskAxes(direction glm.Vec3f) (u, v glm.Vec3f) {
	dir := direction.Normalize()
	axis := glm.Vec3f{1, 0, 0}
	if d := dir.Dot(axis); d > 0.99 || d < -0.99 {
		axis = glm.Vec3f{0, 0, 1}
	}
	// World x, with whatever part of it runs along the light taken out.
	u = axis.Sub(dir.Scale(dir.Dot(axis))).Normalize()
	v = dir.Cross(u)
	return u, v
}

// IsEnabled reports whether the mask takes effect: it has a texture and a size.
func (m LightMask) IsEnabled() bool {
	return m.Texture.IsValid() && m.Size > 0
}

// ID is this light's stable identity, which the renderer keys its shadow resources on.
func (l *DirectionalLight) ID() LightID {
	return l.id
}

// SetCastShadow toggles shadow casting. Turning it on creates the Shadow with the
// default settings; turning it off drops it.
func (l *DirectionalLight) SetCastShadow(on bool) {
	if on {
		if l.shadow == nil {
			l.shadow = newDirectionalShadow()
		}
		return
	}
	l.shadow = nil
}

// Shadow returns the light's shadow settings, or nil if it does not cast shadows.
func (l *DirectionalLight) Shadow() *DirectionalShadow {
	return l.shadow
}

// SetMask sets the light's mask (see LightMask), replacing any earlier one; a zero
// LightMask removes it. The light takes its own reference to the texture, so the
// caller may release theirs.
func (l *DirectionalLight) SetMask(mask LightMask) {
	if mask.Texture.IsValid() {
		mask.Texture = mask.Texture.Copy()
	}
	// Released after the copy, so setting the mask a light already has cannot free it.
	if l.mask.Texture.IsValid() {
		l.mask.Texture.Release()
	}
	l.mask = mask
}

// Mask returns the light's mask, or a zero LightMask if it has none.
func (l *DirectionalLight) Mask() LightMask {
	return l.mask
}

// PointLight is an omnidirectional light at Position with a linear falloff to zero at
// Range. Fields are exported and may be changed at any time.
type PointLight struct {
	Position  glm.Vec3f
	Color     colors.RGB32F
	Intensity float32
	Range     float32
	shadow    *LightShadow
	id        LightID
}

// ID is this light's stable identity, which the renderer keys its shadow resources on.
func (l *PointLight) ID() LightID {
	return l.id
}

// SetCastShadow toggles shadow casting. Turning it on creates six 90° perspective cube
// faces (the renderer aims them from the light each frame); turning it off drops them.
func (l *PointLight) SetCastShadow(on bool) {
	if on {
		if l.shadow == nil {
			l.shadow = newLightShadow()
		}
		return
	}
	l.shadow = nil
}

// Shadow returns the light's shadow settings, or nil if it does not cast shadows.
func (l *PointLight) Shadow() *LightShadow {
	return l.shadow
}

// SpotLight is a cone light at Position aimed along Direction: full intensity inside
// the inner cone, falling to zero at the outer half-angle Angle (radians), with a
// linear distance falloff to zero at Range. Penumbra (0..1) is the fraction of the cone
// used for the soft edge (inner angle = Angle·(1−Penumbra)). Fields are exported and
// may change at any time.
type SpotLight struct {
	Position  glm.Vec3f
	Direction glm.Vec3f
	Color     colors.RGB32F
	Intensity float32
	Range     float32
	Angle     float32 // outer cone half-angle (radians)
	Penumbra  float32 // 0..1 soft-edge fraction
	shadow    *LightShadow
	id        LightID
}

// ID is this light's stable identity, which the renderer keys its shadow resources on.
func (l *SpotLight) ID() LightID {
	return l.id
}

// SetCastShadow toggles shadow casting. Turning it on creates the Shadow with a
// perspective camera matching the cone; turning it off drops it.
func (l *SpotLight) SetCastShadow(on bool) {
	if on {
		if l.shadow == nil {
			l.shadow = newLightShadow()
		}
		return
	}
	l.shadow = nil
}

// Shadow returns the light's shadow settings, or nil if it does not cast shadows.
func (l *SpotLight) Shadow() *LightShadow {
	return l.shadow
}
