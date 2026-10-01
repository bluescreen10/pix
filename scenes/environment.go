package scenes

import (
	"sync/atomic"

	"github.com/bluescreen10/pix/textures"
)

// Environment is the light that surrounds a scene: an image of everything around it,
// as seen from its middle. The renderer lights every surface from it — each takes
// diffuse light from the half of it the surface faces, and reflects the direction it
// mirrors, blurred by its roughness — in place of the scene's flat ambient colour; and
// it can draw it behind the scene, where nothing else is drawn.
//
// The image is equirectangular: u goes once around the vertical, starting from -x and
// passing -z, +x and +z; v runs from straight up (0) to straight down (1). That is how
// panoramic environment images are laid out, so one can be used as it is.
//
// What the renderer derives from it — the diffuse light per direction and the blurred
// reflections — is redone only when the image changes: when it is replaced, or when
// Invalidate says its contents were rewritten, as a sky that draws into it every frame
// does. Intensity, Rotation and Background are read every frame and cost nothing to
// change.
type Environment struct {
	texture  textures.Texture
	revision uint64

	// Intensity scales all of the environment's light.
	Intensity float32
	// Rotation turns the environment about the vertical axis, in radians.
	Rotation float32
	// Background draws the environment behind the scene, wherever no geometry is.
	Background bool
}

// environmentRevisions numbers every change to every environment, so that one
// environment replacing another can never look to a renderer like nothing changed.
var environmentRevisions atomic.Uint64

// NewEnvironment returns an environment lit by texture, an equirectangular image, at
// full intensity. It takes its own reference to the texture.
func NewEnvironment(texture textures.Texture) *Environment {
	e := &Environment{Intensity: 1}
	e.SetTexture(texture)
	return e
}

// Texture returns the environment's image.
func (e *Environment) Texture() textures.Texture {
	return e.texture
}

// SetTexture replaces the environment's image, taking a reference of its own, so the
// caller may release theirs.
func (e *Environment) SetTexture(texture textures.Texture) {
	if texture.IsValid() {
		texture = texture.Copy()
	}
	// Released after the copy, so setting the image an environment already has cannot
	// free it.
	if e.texture.IsValid() {
		e.texture.Release()
	}
	e.texture = texture
	e.Invalidate()
}

// Invalidate says the image's contents have changed — a sky has drawn into it, say —
// so the renderer derives the environment's light from it again.
func (e *Environment) Invalidate() {
	e.revision = environmentRevisions.Add(1)
}

// Release drops the environment's reference to its image. An environment still set on
// a scene keeps being used, so release it once no scene uses it.
func (e *Environment) Release() {
	e.SetTexture(textures.Texture{})
}

// SetEnvironment sets the light surrounding the scene (see Environment); nil, the
// default, lights it with its flat ambient colour instead. The scene does not take
// ownership: the environment is the caller's to release.
func (s *Scene) SetEnvironment(environment *Environment) {
	s.environment = environment
}

// Environment returns the light surrounding the scene, or nil if it has none.
func (s *Scene) Environment() *Environment {
	return s.environment
}

// environmentState resolves an environment to the values a packet carries: nothing
// for none, or one without an image.
func environmentState(e *Environment) EnvironmentMapState {
	if e == nil || !e.texture.IsValid() {
		return EnvironmentMapState{}
	}
	return EnvironmentMapState{
		Texture:    e.texture.Index(),
		Revision:   e.revision,
		Intensity:  e.Intensity,
		Rotation:   e.Rotation,
		Background: e.Background,
	}
}
