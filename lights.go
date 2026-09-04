// Light types: what a user creates and configures (DirectionalLight, PointLight,
// SpotLight) plus the per-light shadow resources. The flat GPU table these are
// compiled into each frame lives in scene_lights.go.
package pix

import (
	"github.com/bluescreen10/pix/cameras"
	"github.com/bluescreen10/pix/colors"
	"github.com/bluescreen10/pix/glm"
)

// defaultShadowSize is the default shadow-map resolution (per side).
const defaultShadowSize uint32 = 1024

// defaultLocalShadowBias is the baseline depth-compare bias, in normalized depth units,
// for lights whose shadow camera is perspective and bounded by the light's Range
// (spot, point). Their depth range is local to the light, so a constant behaves
// consistently — unlike a directional light's orthographic range, which spans the
// scene and must have its bias derived from the fit (see fitDirectionalShadow).
const defaultLocalShadowBias float32 = 0.0015

// LightShadow holds a light's shadow-map resources, created by SetCastShadow(true).
// Camera is the light's view of the scene (orthographic for directional, perspective
// for spot; point lights use six internally) and Map is the depth texture the shadow
// pass renders into and the lit shaders sample — both are engine-managed and exposed
// for inspection. Resolution and bias are settings, so they go through accessors: each
// one has derived state to keep in step (the map's allocation, the normalized bias),
// which a bare field could not maintain.
type LightShadow struct {
	Camera Camera
	Map    Texture

	size uint32  // requested resolution per side
	bias float32 // extra depth offset, in WORLD units

	// ndcBias is bias (+ a term derived from the map's texel footprint) converted into
	// the shadow camera's normalized depth units, which is what the comparison actually
	// needs. Recomputed whenever the fit changes: an orthographic shadow camera's depth
	// range spans the scene, so a constant in NDC is a wildly different distance in
	// world units from one scene to the next.
	ndcBias float32
	// mapSize is the resolution Map (and each face map) was actually created at, so a
	// change to size can be noticed and the map reallocated.
	mapSize uint32
	// Point lights render six cube faces instead of one map; faces holds their
	// per-face camera + depth map. nil for directional/spot (which use Camera/Map).
	faces []pointFace
}

// Size is the shadow map's resolution per side.
func (s *LightShadow) Size() uint32 { return s.size }

// SetSize sets the shadow map's resolution per side. The map is reallocated at the new
// resolution on the next frame that renders it; a zero size is ignored.
func (s *LightShadow) SetSize(size uint32) {
	if size == 0 {
		return
	}
	s.size = size
}

// Bias is the extra depth offset applied to the shadow comparison, in world units.
func (s *LightShadow) Bias() float32 { return s.bias }

// SetBias sets an extra depth offset for the shadow comparison, in WORLD units, on top
// of a bias derived from the map's texel footprint. 0 (the default) is usually right —
// the derived term already scales with the fit, so it works at any scene scale. Raise
// this if surfaces self-shadow (acne); it takes effect on the next frame.
func (s *LightShadow) SetBias(bias float32) { s.bias = bias }

// ensureMap allocates the depth map, or reallocates it when SetSize changed the
// requested resolution. Point lights use ensureFaceMaps instead.
func (s *LightShadow) ensureMap(tex *textureSystem) {
	if s.Map.Valid() && s.mapSize == s.size {
		return
	}
	if s.Map.Valid() {
		s.Map.Release()
	}
	s.Map = tex.createDepthTarget(s.size, s.size)
	s.mapSize = s.size
}

// ensureFaceMaps is ensureMap for a point light's six cube faces, which share one
// resolution.
func (s *LightShadow) ensureFaceMaps(tex *textureSystem) {
	stale := s.mapSize != s.size
	for i := range s.faces {
		f := &s.faces[i]
		if f.m.Valid() && !stale {
			continue
		}
		if f.m.Valid() {
			f.m.Release()
		}
		f.m = tex.createDepthTarget(s.size, s.size)
	}
	s.mapSize = s.size
}

// pointFace is one cube face of a point light's shadow: a perspective camera aimed
// down a ±axis and the depth map it renders into.
type pointFace struct {
	cam Camera
	m   Texture
}

func newLightShadow(cam Camera) *LightShadow {
	return &LightShadow{Camera: cam, size: defaultShadowSize}
}

// updateOrthoBias recomputes ndcBias for an orthographic (directional) shadow camera
// whose fitted box is radius wide and whose depth range is depthRange, both in world
// units. Acne scales with how much world space a single shadow texel covers, so that
// is the natural unit for the derived term; Bias adds an explicit world-space offset.
//
// The division is the whole point: an orthographic shadow camera's depth range spans
// the scene, so a bias expressed directly in normalized depth silently becomes a
// wildly different world distance from one scene to the next.
func (s *LightShadow) updateOrthoBias(radius, depthRange float32) {
	if depthRange <= 0 {
		s.ndcBias = 0
		return
	}
	texel := 2 * radius / float32(s.size)
	s.ndcBias = (texel*1.5 + s.bias) / depthRange
}

// updateLocalBias recomputes ndcBias for a perspective shadow camera bounded by a
// light's range (spot, point). Perspective depth is non-linear, so there is no exact
// world→normalized factor — scaling Bias by the range is the approximation, chosen so
// Bias means something for every light type rather than being ignored on these two.
func (s *LightShadow) updateLocalBias(rng float32) {
	s.ndcBias = defaultLocalShadowBias
	if rng > 0 {
		s.ndcBias += s.bias / rng
	}
}

// DirectionalLight is a distant light with parallel rays (a sun). Direction is the
// direction the light travels (e.g. {0,-1,0} for a downward sun). Fields are exported
// and may be changed at any time; the scene re-derives the GPU light table each frame.
type DirectionalLight struct {
	Direction glm.Vec3f
	Color     colors.RGB32F
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
	Color     colors.RGB32F
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
	Color     colors.RGB32F
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
