// Volumetric fog: the volume of froxels the renderer lays over the camera's view and
// fills with fog every frame, when a scene's fog is a scenes.VolumetricFog.
package pix

import "github.com/bluescreen10/pix/glm"

// VolumetricFogSettings is how finely the renderer simulates volumetric fog: the
// resolution of the volume of froxels it lays over the camera's view. Width and Height
// divide the screen, Depth the distance out to the fog's reach. A zero field takes its
// default — 160, 90 and 64 — so the zero value is a sensible setting.
type VolumetricFogSettings struct {
	Width, Height, Depth uint32
}

// defaultVolumetricFog is what each zero field of a VolumetricFogSettings takes.
var defaultVolumetricFog = VolumetricFogSettings{Width: 160, Height: 90, Depth: 64}

// resolved returns the settings with every zero field replaced by its default.
func (s VolumetricFogSettings) resolved() VolumetricFogSettings {
	if s.Width == 0 {
		s.Width = defaultVolumetricFog.Width
	}
	if s.Height == 0 {
		s.Height = defaultVolumetricFog.Height
	}
	if s.Depth == 0 {
		s.Depth = defaultVolumetricFog.Depth
	}
	return s
}

// fogInjectRoot matches PC in fog_inject.comp.glsl.
type fogInjectRoot struct {
	inverseViewProj glm.Mat4f
	lights          uint64
	eye             glm.Vec3f
	density         float32
	albedo          glm.Vec3f
	anisotropy      float32
	emission        glm.Vec3f
	baseHeight      float32
	heightFalloff   float32
	reach           float32
	medium          uint32
	shadowSampler   uint32
	size            [3]uint32
	_               [3]uint32
}

// fogIntegrateRoot matches PC in fog_integrate.comp.glsl.
type fogIntegrateRoot struct {
	medium, volume, sampler uint32
	reach                   float32
	size                    [3]uint32
	_                       uint32
}

// fogBackgroundParams follows POSTFX_ROOT in fog_background.frag.glsl.
type fogBackgroundParams struct {
	volume uint32
	// lastSlice is the volume's last slice, as a depth coordinate.
	lastSlice float32
	_         [2]uint32
}

// fogLookup is what lit shaders need to look volumetric fog up (see applyFog): the fog
// volume's heap index, the sampler to read it with, its depth in slices, and the size
// of the screen it lies over. A zero fogLookup means there is no volume to look up.
type fogLookup struct {
	volume, sampler, slices uint32
	width, height           uint32
}
