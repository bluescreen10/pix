package pix_test

import (
	"math"
	"testing"

	"github.com/bluescreen10/pix"
	"github.com/bluescreen10/pix/colors"
	"github.com/bluescreen10/pix/geometries"
	"github.com/bluescreen10/pix/glm"
	"github.com/bluescreen10/pix/materials"
	"github.com/bluescreen10/pix/scenes"
)

// fogScene is a scene with the given fog, a black background and no ambient light, seen
// from the origin down -z.
func fogScene(t *testing.T, fog scenes.Fog) (*pix.Renderer, *scenes.Scene) {
	t.Helper()
	r, err := pix.NewOffscreenRenderer(postSize, postSize)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(r.Destroy)
	r.SetClearColor(colors.RGBA32F{0, 0, 0, 1})

	scene := scenes.New()
	t.Cleanup(scene.Destroy)
	scene.SetAmbient(colors.RGB32F{}, 1)
	scene.SetFog(fog)
	cam := scene.NewPerspectiveCamera(45, 1, 0.1, 200)
	cam.LookAt(glm.Vec3f{0, 0, -1})
	return r, scene
}

// addWall adds an unlit square of the given colour and half-size facing the camera at
// depth z.
func addWall(r *pix.Renderer, scene *scenes.Scene, color colors.RGBA32F, halfSize, z float32) {
	h := halfSize
	quad := r.GeometryStore.Create(geometries.GeometryConfig{
		Attributes: []geometries.Attribute{
			geometries.NewAttribute(geometries.AttributePosition, geometries.Float32x3, []glm.Vec3f{{-h, -h, z}, {h, -h, z}, {h, h, z}, {-h, h, z}}),
		},
		Indices: []uint32{0, 1, 2, 0, 2, 3},
	})
	material := r.NewBasicMaterial()
	material.SetColor(color)
	material.SetCull(materials.CullNone)
	scene.NewMesh(quad, material)
}

// isNear reports whether a rendered byte is within tolerance of the display encoding
// of linear light v: the volume is sampled between slices, so its values are close,
// not exact.
func isNear(got byte, v float64, tolerance int) bool {
	return math.Abs(float64(int(got)-srgbByte(float32(v)))) <= float64(tolerance)
}

// TestVolumetricFogDimsSurfaceByTransmittance: fog that only absorbs lets
// exp(-distance/visibility) of a surface through. A white surface 10 units away, in fog
// you can see 10 units into, shows at exp(-1).
func TestVolumetricFogDimsSurfaceByTransmittance(t *testing.T) {
	fog := scenes.NewVolumetricFog(10, 50)
	fog.Albedo = colors.RGB32F{}
	r, scene := fogScene(t, fog)
	addWall(r, scene, colors.RGBA32F{1, 1, 1, 1}, 5, -10)

	r.Render(scene)

	want := math.Exp(-1)
	if got := pixelAt(r, postSize/2, postSize/2); !isNear(got[0], want, 4) || got[0] != got[1] || got[1] != got[2] {
		t.Errorf("center = %v, want grey %d, the white surface dimmed to exp(-1)", got, srgbByte(float32(want)))
	}
}

// TestVolumetricFogFillsTheBackground: where no geometry is, the fog still lies between
// the camera and the background, out to its reach. Fog that only glows adds
// emission·(1 - exp(-reach/visibility)): emission 1, visibility 20 and reach 20 give
// 1 - exp(-1) over a black background.
func TestVolumetricFogFillsTheBackground(t *testing.T) {
	fog := scenes.NewVolumetricFog(20, 20)
	fog.Albedo = colors.RGB32F{}
	fog.Emission = colors.RGB32F{1, 1, 1}
	r, scene := fogScene(t, fog)

	r.Render(scene)

	want := 1 - math.Exp(-1)
	if got := pixelAt(r, postSize/2, postSize/2); !isNear(got[0], want, 4) || got[0] != got[1] || got[1] != got[2] {
		t.Errorf("background = %v, want grey %d, the fog's glow out to its reach", got, srgbByte(float32(want)))
	}
}

// TestVolumetricFogGlowsAroundPointLight: a point light lights the fog within its
// range. The ray through the screen's centre passes through the light; the ray through
// a corner passes no nearer than 4 units to it, beyond its range of 3, so it sees no
// light at all.
func TestVolumetricFogGlowsAroundPointLight(t *testing.T) {
	r, scene := fogScene(t, scenes.NewVolumetricFog(10, 30))
	scene.AddPointLight(glm.Vec3f{0, 0, -8}, colors.RGB32F{1, 1, 1}, 5, 3)

	r.Render(scene)

	center, corner := pixelAt(r, postSize/2, postSize/2), pixelAt(r, 0, 0)
	if center[0] < 40 || corner[0] > 2 {
		t.Errorf("center = %v, corner = %v, want the fog lit around the light and dark out of its range", center, corner)
	}
}

// TestVolumetricFogShowsSpotLightCone: a spot light lights the fog inside its cone and
// nowhere else. It points straight down from 4 units above the screen's centre, so the
// centre's ray crosses the cone; the ray through the middle of the left edge passes no
// nearer than 3 units to its axis, well outside it.
func TestVolumetricFogShowsSpotLightCone(t *testing.T) {
	r, scene := fogScene(t, scenes.NewVolumetricFog(10, 30))
	scene.AddSpotLight(glm.Vec3f{0, 4, -8}, glm.Vec3f{0, -1, 0}, colors.RGB32F{1, 1, 1}, 5, 10, 0.25, 0.2)

	r.Render(scene)

	center, left := pixelAt(r, postSize/2, postSize/2), pixelAt(r, 0, postSize/2)
	if center[0] < 20 || left[0] > 2 {
		t.Errorf("center = %v, left = %v, want the fog lit inside the cone and dark outside it", center, left)
	}
}

// TestVolumetricFogScattersForward: with anisotropy, fog scatters more of the light
// onward than back, so looking toward the sun shows more of it than looking away.
func TestVolumetricFogScattersForward(t *testing.T) {
	fog := scenes.NewVolumetricFog(20, 30)
	fog.Anisotropy = 0.8
	r, scene := fogScene(t, fog)
	sun := scene.AddDirectionalLight(glm.Vec3f{0, 0, 1}, colors.RGB32F{1, 1, 1}, 1)

	r.Render(scene) // the sun is ahead, its light coming at the camera
	toward := pixelAt(r, postSize/2, postSize/2)[0]
	sun.Direction = glm.Vec3f{0, 0, -1} // the sun is behind the camera
	r.Render(scene)
	away := pixelAt(r, postSize/2, postSize/2)[0]

	if away == 0 || toward < 2*away {
		t.Errorf("looking toward the sun = %d, away from it = %d, want toward at least twice as bright", toward, away)
	}
}

// TestVolumetricFogFollowsShadows: fog in a light's shadow is not lit by it. The sun
// shines toward the camera from behind a black wall, so the fog between the camera and
// the wall is in the wall's shadow: with shadows on, looking at the wall shows almost
// none of the sun in the fog; with them off, the fog in front is lit. Not none at all:
// the slice the wall lies in also reaches just behind it, where the fog is lit, and a
// lookup between slices takes a little of that.
func TestVolumetricFogFollowsShadows(t *testing.T) {
	r, scene := fogScene(t, scenes.NewVolumetricFog(20, 30))
	addWall(r, scene, colors.RGBA32F{0, 0, 0, 1}, 20, -12)
	sun := scene.AddDirectionalLight(glm.Vec3f{0, 0, 1}, colors.RGB32F{1, 1, 1}, 1)
	sun.SetCastShadow(true)

	r.EnableShadows(true)
	r.Render(scene)
	shadowed := pixelAt(r, postSize/2, postSize/2)[0]
	r.EnableShadows(false)
	r.Render(scene)
	lit := pixelAt(r, postSize/2, postSize/2)[0]

	if lit < 20 || int(shadowed)*8 > int(lit) {
		t.Errorf("fog in front of the wall = %d with shadows, %d without, want a fraction of it in the shadow", shadowed, lit)
	}
}

// TestVolumetricFogResolutionIsTheRenderers: the renderer decides how finely the fog
// is simulated. A volume one froxel wide and high gives every column the same fog, so
// the corner shows what the centre does, where at the default resolution it is dark.
func TestVolumetricFogResolutionIsTheRenderers(t *testing.T) {
	r, scene := fogScene(t, scenes.NewVolumetricFog(10, 30))
	scene.AddPointLight(glm.Vec3f{0, 0, -8}, colors.RGB32F{1, 1, 1}, 5, 3)
	if got := r.VolumetricFog(); got != (pix.VolumetricFogSettings{}) {
		t.Errorf("VolumetricFog() = %+v, want the zero value until set", got)
	}

	r.SetVolumetricFog(pix.VolumetricFogSettings{Width: 1, Height: 1})
	r.Render(scene)

	center, corner := pixelAt(r, postSize/2, postSize/2), pixelAt(r, 0, 0)
	if center[0] == 0 || corner != center {
		t.Errorf("center = %v, corner = %v, want one column's fog everywhere", center, corner)
	}
}
