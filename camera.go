package pix

import "github.com/bluescreen10/pix/glm"

// Camera is a view of the scene: a world→clip transform plus the eye position (for
// specular). Concrete cameras live in the cameras package (PerspectiveCamera for the
// main view / spot & point shadows, OrthographicCamera for directional shadows); a
// frame can be rendered from any camera, and a shadow view holds one internally.
type Camera interface {
	ViewProjection() glm.Mat4f
	Position() glm.Vec3f
}
