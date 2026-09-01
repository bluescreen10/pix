package pix

import (
	"github.com/bluescreen10/pix/cameras"
	"github.com/bluescreen10/pix/glm"
)

// defaultShadowSize is the default shadow-map resolution (per side).
const defaultShadowSize uint32 = 1024

// Shadow holds a light's shadow-map resources, created by SetCastShadow(true). Camera
// is the light's view of the scene (orthographic for directional, perspective for
// spot; point lights use six internally). Map is the depth texture the shadow pass
// renders into and the lit shaders sample. Size is its resolution; Bias offsets the
// depth compare to fight acne.
//
// Phase 0: the Camera + config exist, but the Map is allocated (and rendered) by the
// renderer once shadow rendering lands — see project_shadow_maps.
type LightShadow struct {
	Camera Camera
	Map    Texture
	Size   uint32
	Bias   float32
	// Point lights render six cube faces instead of one map; faces holds their
	// per-face camera + depth map. nil for directional/spot (which use Camera/Map).
	faces []pointFace
}

// pointFace is one cube face of a point light's shadow: a perspective camera aimed
// down a ±axis and the depth map it renders into.
type pointFace struct {
	cam Camera
	m   Texture
}

func newLightShadow(cam Camera) *LightShadow {
	return &LightShadow{Camera: cam, Size: defaultShadowSize, Bias: 0.002}
}

// DirectionalLight is a distant light with parallel rays (a sun). Direction is the
// direction the light travels (e.g. {0,-1,0} for a downward sun). Fields are exported
// and may be changed at any time; the scene re-derives the GPU light table each frame.
type DirectionalLight struct {
	Direction glm.Vec3f
	Color     glm.Vec3f
	Intensity float32
	shadow    *LightShadow
}

// SetCastShadow toggles shadow casting. Turning it on creates the Shadow with an
// orthographic camera; turning it off drops it.
func (l *DirectionalLight) SetCastShadow(on bool) {
	switch {
	case on && l.shadow == nil:
		// Orthographic view; the renderer fits the frustum to the scene each frame.
		cam := cameras.NewOrthographicCamera(-10, 10, -10, 10, 0.1, 100)
		l.shadow = newLightShadow(cam)
	case !on:
		l.shadow = nil
	}
}

// Shadow returns the light's shadow resources, or nil if it does not cast shadows.
func (l *DirectionalLight) Shadow() *LightShadow {
	return l.shadow
}

// PointLight is an omnidirectional light at Position with a linear falloff to zero at
// Range. Fields are exported and may be changed at any time.
type PointLight struct {
	Position  glm.Vec3f
	Color     glm.Vec3f
	Intensity float32
	Range     float32
	shadow    *LightShadow
}

// cubeFaceDirs/cubeFaceUps are the six 90° cube-face view directions and their up
// vectors (the ±Y faces use a Z up to avoid a degenerate look-at). Face order matches
// the shader's dominant-axis selection: +X,-X,+Y,-Y,+Z,-Z.
var cubeFaceDirs = [6]glm.Vec3f{{1, 0, 0}, {-1, 0, 0}, {0, 1, 0}, {0, -1, 0}, {0, 0, 1}, {0, 0, -1}}
var cubeFaceUps = [6]glm.Vec3f{{0, 1, 0}, {0, 1, 0}, {0, 0, 1}, {0, 0, -1}, {0, 1, 0}, {0, 1, 0}}

// SetCastShadow toggles shadow casting. Turning it on creates six 90° perspective cube
// faces (the renderer aims them from the light each frame); turning it off drops them.
func (l *PointLight) SetCastShadow(on bool) {
	switch {
	case on && l.shadow == nil:
		s := newLightShadow(nil)
		s.faces = make([]pointFace, 6)
		for i := range s.faces {
			s.faces[i].cam = cameras.NewPerspectiveCamera(90, 1, 0.05, l.Range)
		}
		l.shadow = s
	case !on:
		l.shadow = nil
	}
}

// Shadow returns the light's shadow resources, or nil if it does not cast shadows.
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
	Color     glm.Vec3f
	Intensity float32
	Range     float32
	Angle     float32 // outer cone half-angle (radians)
	Penumbra  float32 // 0..1 soft-edge fraction
	shadow    *LightShadow
}

// SetCastShadow toggles shadow casting. Turning it on creates the Shadow with a
// perspective camera matching the cone; turning it off drops it.
func (l *SpotLight) SetCastShadow(on bool) {
	switch {
	case on && l.shadow == nil:
		// Perspective view down the cone: full-angle FOV, square aspect. The renderer
		// re-aims it (position/target/FOV/range) each frame from the light's fields.
		fov := glm.ToDegrees(2 * l.Angle)
		cam := cameras.NewPerspectiveCamera(fov, 1, 0.05, l.Range)
		l.shadow = newLightShadow(cam)
	case !on:
		l.shadow = nil
	}
}

// Shadow returns the light's shadow resources, or nil if it does not cast shadows.
func (l *SpotLight) Shadow() *LightShadow {
	return l.shadow
}
