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

type lightShadow struct {
	bias    float32
	mapSize glm.Vec2i
}
