package pix

import "github.com/bluescreen10/pix/cameras"

type DirectionalShadow struct {
	Camera *cameras.OrthographicCamera
	Bias   float32
}

// NewDirectionalShadow creates a shadow with an orthographic frustum of ±size
// in both axes and the given depth range and shadow-map resolution.
func NewDirectionalShadow(size, near, far float32) *DirectionalShadow {
	return &DirectionalShadow{
		Camera: cameras.NewOrthographicCamera(-size, size, -size, size, near, far),
		Bias:   0.005,
	}
}
