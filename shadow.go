package pix

import (
	"github.com/bluescreen10/dawn-go/wgpu"
	"github.com/bluescreen10/pix/cameras"
	"github.com/bluescreen10/pix/glm"
)

// DirectionalShadow holds the shadow configuration and the shadow map for a directional light.
// target is nil until the renderer first renders shadows for this light.
type DirectionalShadow struct {
	lightShadow
	camera *cameras.OrthographicCamera
}

// NewDirectionalShadow creates a shadow with an orthographic frustum of ±size
// in both axes and the given depth range.
func NewDirectionalShadow(size, near, far float32) *DirectionalShadow {
	return &DirectionalShadow{
		lightShadow: lightShadow{
			bias:    0.005,
			mapSize: glm.Vec2i{DefaultShadowMapSize, DefaultShadowMapSize},
		},
		camera: cameras.NewOrthographicCamera(-size, size, -size, size, near, far),
	}
}

type lightShadow struct {
	bias    float32
	mapSize glm.Vec2i
	target  *wgpu.TextureView
}
