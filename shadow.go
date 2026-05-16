package pix

import (
	"github.com/bluescreen10/pix/cameras"
	"github.com/bluescreen10/pix/glm"
)

// DirectionalShadow holds the shadow configuration and the shadow map for a directional light.
type DirectionalShadow struct {
	lightShadow
	camera *cameras.OrthographicCamera
}

// NewDirectionalShadow creates a shadow with an orthographic frustum of ±size
// in both axes and the given depth range.
func NewDirectionalShadow(size, near, far float32) *DirectionalShadow {
	return &DirectionalShadow{
		lightShadow: lightShadow{
			bias:    0.001,
			mapSize: glm.Vec2i{DefaultShadowMapSize, DefaultShadowMapSize},
		},
		camera: cameras.NewOrthographicCamera(-size, size, -size, size, near, far),
	}
}

// SetSize resizes the orthographic shadow frustum to ±size on both axes.
// Smaller values concentrate the shadow map resolution over a tighter area,
// reducing visible pixelation and peter panning at object edges.
func (s *DirectionalShadow) SetSize(size float32) {
	s.camera.SetFrustum(-size, size, -size, size)
}

// SetBias sets the depth-comparison bias used to prevent self-shadowing acne.
func (s *DirectionalShadow) SetBias(bias float32) {
	s.bias = bias
}

// SpotShadow holds the shadow configuration and shadow map for a spot light.
type SpotShadow struct {
	lightShadow
	camera *cameras.PerspectiveCamera
}

// NewSpotShadow creates a shadow with a perspective frustum matching the spot
// light's outer cone angle. The camera uses aspect ratio 1.0 for the square
// shadow map; fov is outerAngle * 2.
func NewSpotShadow(outerAngle, near, far float32) *SpotShadow {
	return &SpotShadow{
		lightShadow: lightShadow{
			bias:    0.001,
			mapSize: glm.Vec2i{DefaultShadowMapSize, DefaultShadowMapSize},
		},
		camera: cameras.NewPerpectiveCamera(glm.ToRadians(outerAngle)*2, 1.0, near, far),
	}
}

// SetBias sets the depth-comparison bias used to prevent self-shadowing acne.
func (s *SpotShadow) SetBias(bias float32) { s.bias = bias }

// PointShadow holds the shadow configuration for a point light (omnidirectional).
type PointShadow struct {
	lightShadow
	far float32
}

// NewPointShadow creates a point-light shadow with the given far plane.
// The near plane is fixed at 0.1. Shadow maps use a cube array (6 faces).
func NewPointShadow(far float32) *PointShadow {
	return &PointShadow{
		lightShadow: lightShadow{
			bias:    0.05,
			mapSize: glm.Vec2i{DefaultShadowMapSize, DefaultShadowMapSize},
		},
		far: far,
	}
}

func (s *PointShadow) SetBias(bias float32) { s.bias = bias }
func (s *PointShadow) SetFar(far float32)   { s.far = far }

type lightShadow struct {
	bias    float32
	mapSize glm.Vec2i
}
