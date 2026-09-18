// Light types: what a user creates and configures (DirectionalLight, PointLight,
// SpotLight) plus the per-light shadow resources. The flat GPU table these are
// compiled into each frame lives in scene_lights.go.
package scenes

import (
	"github.com/bluescreen10/pix/colors"
	"github.com/bluescreen10/pix/glm"
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

// DirectionalLight is a distant light with parallel rays (a sun). Direction is the
// direction the light travels (e.g. {0,-1,0} for a downward sun). Fields are exported
// and may be changed at any time; the scene re-derives the GPU light table each frame.
type DirectionalLight struct {
	Direction glm.Vec3f
	Color     colors.RGB32F
	Intensity float32
	shadow    *LightShadow
	id        LightID
}

// ID is this light's stable identity, which the renderer keys its shadow resources on.
func (l *DirectionalLight) ID() LightID {
	return l.id
}

// SetCastShadow toggles shadow casting. Turning it on creates the Shadow with an
// orthographic camera; turning it off drops it.
func (l *DirectionalLight) SetCastShadow(on bool) {
	if on {
		if l.shadow == nil {
			l.shadow = newLightShadow()
		}
		return
	}
	l.shadow = nil
}

// Shadow returns the light's shadow settings, or nil if it does not cast shadows.
func (l *DirectionalLight) Shadow() *LightShadow {
	return l.shadow
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
