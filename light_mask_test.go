package pix_test

import (
	"testing"

	"github.com/bluescreen10/pix"
	"github.com/bluescreen10/pix/colors"
	"github.com/bluescreen10/pix/glm"
	"github.com/bluescreen10/pix/materials"
	"github.com/bluescreen10/pix/scenes"
	"github.com/bluescreen10/pix/textures"
)

// maskTexture is a one-row mask of the given fractions of light let through, one texel
// each.
func maskTexture(r *pix.Renderer, fractions ...byte) textures.Texture {
	rgba := make([]byte, 0, len(fractions)*4)
	for _, f := range fractions {
		rgba = append(rgba, f, f, f, 255)
	}
	return r.TextureStore.Create(nrgbaImage(rgba, len(fractions), 1), textures.Linear)
}

// groundScene is a white ground at y = 0, lit through the given material type, under a
// sun shining straight down, seen from above by an orthographic camera that frames x
// from 0 to 8.
func groundScene(t *testing.T, newMaterial func(*pix.Renderer) materials.Material) (*pix.Renderer, *scenes.Scene, *scenes.DirectionalLight) {
	t.Helper()
	r, err := pix.NewOffscreenRenderer(postSize, postSize)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(r.Destroy)
	scene := scenes.New()
	t.Cleanup(scene.Destroy)
	scene.SetAmbient(colors.RGB32F{0.1, 0.1, 0.1}, 1)
	sun := scene.AddDirectionalLight(glm.Vec3f{0, -1, 0}, colors.RGB32F{1, 1, 1}, 1)

	scene.NewMesh(r.NewPlaneGeometry(40, 40, 1, 1), newMaterial(r))
	cam := scene.NewOrthographicCamera(-4, 4, -4, 4, 0.1, 100)
	cam.SetPosition(glm.Vec3f{4, 10, 0.01})
	cam.LookAt(glm.Vec3f{4, 0, 0})
	return r, scene, sun
}

// The two probes: ground at x = 2 and x = 6, on the middle row. Which is which on screen
// is the camera's business, so the tests below only rely on them being half a mask
// repeat apart.
var groundProbes = [2][2]int{{postSize / 4, postSize / 2}, {3 * postSize / 4, postSize / 2}}

// The lit material types, each of which applies a light's mask in its own shader.
var litMaterials = []struct {
	name string
	new  func(*pix.Renderer) materials.Material
}{
	{"BlinnPhong", func(r *pix.Renderer) materials.Material {
		m := r.NewBlinnPhongMaterial()
		m.SetColor(colors.RGBA32F{1, 1, 1, 1})
		return m
	}},
	{"PBR", func(r *pix.Renderer) materials.Material {
		m := r.NewPBRMaterial()
		m.SetColor(colors.RGBA32F{1, 1, 1, 1})
		return m
	}},
}

// groundAt renders the scene and reads both probes.
func groundAt(r *pix.Renderer, scene *scenes.Scene) [2]byte {
	r.Render(scene)
	return [2]byte{pixelAt(r, groundProbes[0][0], groundProbes[0][1])[0], pixelAt(r, groundProbes[1][0], groundProbes[1][1])[0]}
}

// isWithin reports whether two rendered bytes differ by no more than tolerance.
func isWithin(a, b byte, tolerance int) bool {
	d := int(a) - int(b)
	return d >= -tolerance && d <= tolerance
}

// TestLightMaskShadesAcrossTheLight lays a mask of one black and one white texel,
// repeating every 8 units, under a sun shining straight down, so that ground at x = 2
// is under the black texel and ground at x = 6 under the white one. The first must be
// lit by the ambient light alone, the second as if there were no mask; and moving the
// mask half a repeat along x must trade them over. Every lit material type applies it.
func TestLightMaskShadesAcrossTheLight(t *testing.T) {
	for _, material := range litMaterials {
		t.Run(material.name, func(t *testing.T) {
			testLightMaskShadesAcrossTheLight(t, material.new)
		})
	}
}

func testLightMaskShadesAcrossTheLight(t *testing.T, newMaterial func(*pix.Renderer) materials.Material) {
	r, scene, sun := groundScene(t, newMaterial)
	unmasked := groundAt(r, scene)
	sun.Intensity = 0
	ambientOnly := groundAt(r, scene)
	sun.Intensity = 1

	mask := maskTexture(r, 0, 255)
	defer mask.Release()
	sun.SetMask(scenes.LightMask{Texture: mask, Size: 8})
	masked := groundAt(r, scene)

	dark, lit := 0, 1
	if masked[0] > masked[1] {
		dark, lit = 1, 0
	}
	if !isWithin(masked[dark], ambientOnly[dark], 6) || !isWithin(masked[lit], unmasked[lit], 6) {
		t.Errorf("masked ground = %v, want one probe as lit by ambient alone (%v) and the other as without the mask (%v)", masked, ambientOnly, unmasked)
	}

	sun.SetMask(scenes.LightMask{Texture: mask, Size: 8, Offset: glm.Vec3f{4, 0, 0}})
	moved := groundAt(r, scene)
	if !isWithin(moved[dark], masked[lit], 3) || !isWithin(moved[lit], masked[dark], 3) {
		t.Errorf("after moving the mask half a repeat, ground = %v, want %v with the probes traded", moved, masked)
	}
}

// TestLightMaskShadesVolumetricFog: a mask stops the light in the fog as it does on
// surfaces, so light shafts follow cloud shadows. With a black mask the sun lights none
// of the fog; without one it does.
func TestLightMaskShadesVolumetricFog(t *testing.T) {
	r, scene := fogScene(t, scenes.NewVolumetricFog(20, 30))
	sun := scene.AddDirectionalLight(glm.Vec3f{0, 0, 1}, colors.RGB32F{1, 1, 1}, 1)
	r.Render(scene)
	unmasked := pixelAt(r, postSize/2, postSize/2)[0]

	mask := maskTexture(r, 0)
	defer mask.Release()
	sun.SetMask(scenes.LightMask{Texture: mask, Size: 10})
	r.Render(scene)
	masked := pixelAt(r, postSize/2, postSize/2)[0]

	if unmasked < 20 || masked > 2 {
		t.Errorf("fog lit by the sun = %d without a mask, %d under a black one, want lit and then dark", unmasked, masked)
	}
}

// TestLightMaskHoldsItsTexture: a light keeps its mask's texture alive after the caller
// lets go of it, and lets go of it in turn when the mask is removed.
func TestLightMaskHoldsItsTexture(t *testing.T) {
	r, scene, sun := groundScene(t, litMaterials[0].new)
	mask := maskTexture(r, 128)
	sun.SetMask(scenes.LightMask{Texture: mask, Size: 8})
	mask.Release()

	if !sun.Mask().Texture.IsValid() {
		t.Fatal("the mask's texture was freed while the light still uses it")
	}
	held := sun.Mask().Texture
	r.Render(scene)

	sun.SetMask(scenes.LightMask{})
	if held.IsValid() {
		t.Error("the mask's texture is still alive after the light, its last holder, removed it")
	}
	if sun.Mask().IsEnabled() {
		t.Error("Mask().IsEnabled() = true after removing the mask")
	}
}
