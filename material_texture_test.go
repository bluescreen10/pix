package pix_test

import (
	"testing"

	"github.com/bluescreen10/pix"
	"github.com/bluescreen10/pix/colors"
	"github.com/bluescreen10/pix/glm"
	"github.com/bluescreen10/pix/scenes"
	"github.com/bluescreen10/pix/textures"
)

// Reproduces what loaders/gltf does: one cached texture handed to two map slots.
func TestSharedTextureOwnership(t *testing.T) {
	r, _ := pix.NewOffscreenRenderer(16, 16)
	defer r.Destroy()

	tex := r.TextureStore.Create(nrgbaImage([]byte{255, 255, 255, 255}, 1, 1), textures.Linear)
	m := r.NewPBRMaterial()
	m.SetMetallicMap(tex)
	m.SetRoughnessMap(tex) // same handle into a second slot
	m.Release()

	if !tex.IsValid() {
		t.Fatal("the caller's own texture reference was freed by the material")
	}
	tex.Release()
}

// TestSetMapSelfRebind pins the reason every SetXMap copies the incoming texture
// BEFORE releasing the one it is replacing: rebinding a map to the texture it already
// holds (m.SetColorMap(m.ColorMap())) is a valid, if unusual, caller pattern. Releasing
// first can drop the shared refcount to 0 and dispose the texture; the following Copy
// then re-bumps a refcount whose slot the store has already retired, leaving the
// material holding a corrupted Ref (see TestSetMapSelfRebindThenReleaseIsSafe for the
// worse consequence of that).
func TestSetMapSelfRebind(t *testing.T) {
	r, err := pix.NewOffscreenRenderer(16, 16)
	if err != nil {
		t.Fatal(err)
	}
	defer r.Destroy()

	tex := r.TextureStore.Create(nrgbaImage([]byte{255, 255, 255, 255}, 1, 1), textures.Linear)
	m := r.NewBasicMaterial()
	m.SetColorMap(tex)
	tex.Release() // the material should hold the only remaining reference

	m.SetColorMap(m.ColorMap()) // rebind to itself

	if !m.ColorMap().IsValid() {
		t.Fatal("self-rebind destroyed the texture")
	}
}

// TestSetMapSelfRebindThenReleaseIsSafe is the sharper form of the same hazard: a
// release-before-copy ordering does not just invalidate the self-rebound handle, it
// leaks its refcount to a nonzero value tied to a retired generation. Releasing the
// material later then disposes whatever texture has since been allocated into that
// same store slot — a completely unrelated resource.
func TestSetMapSelfRebindThenReleaseIsSafe(t *testing.T) {
	r, err := pix.NewOffscreenRenderer(16, 16)
	if err != nil {
		t.Fatal(err)
	}
	defer r.Destroy()

	tex := r.TextureStore.Create(nrgbaImage([]byte{255, 255, 255, 255}, 1, 1), textures.Linear)
	m := r.NewBasicMaterial()
	m.SetColorMap(tex)
	tex.Release()
	m.SetColorMap(m.ColorMap())

	other := r.TextureStore.Create(nrgbaImage([]byte{0, 0, 0, 255}, 1, 1), textures.Linear)
	defer other.Release()

	m.Release()

	if !other.IsValid() {
		t.Fatal("releasing the material after a self-rebind disposed an unrelated texture")
	}
}

// TestEmissiveMap renders a black plate, lit by nothing, that emits white through a red
// emissive map: it glows red. glTF multiplies a material's emissive factor by its map,
// and a lamp's glass, which emits through one, is dark without it.
func TestEmissiveMap(t *testing.T) {
	const size = 16
	r, err := pix.NewOffscreenRenderer(size, size)
	if err != nil {
		t.Fatal(err)
	}
	defer r.Destroy()
	r.SetClearColor(colors.RGBA32F{0, 0, 0, 1})
	scene := scenes.New()
	defer scene.Destroy()
	scene.SetAmbient(colors.RGB32F{}, 0)

	plate := r.NewPBRMaterial()
	plate.SetColor(colors.RGBA32F{0, 0, 0, 1})
	plate.SetMetallic(0)
	plate.SetRoughness(1)
	plate.SetEmissive(colors.RGB32F{1, 1, 1})
	red := r.TextureStore.Create(nrgbaImage([]byte{255, 0, 0, 255}, 1, 1), textures.SRGB)
	defer red.Release()
	plate.SetEmissiveMap(red)
	plate.SetEmissiveMapSampler(r.TextureStore.DefaultSampler())
	scene.Add(scene.NewMesh(r.NewBoxGeometry(2, 2, 0.1), plate))

	cam := scene.NewPerspectiveCamera(45, 1, 0.1, 100)
	scene.Add(cam)
	cam.SetPosition(glm.Vec3f{0, 0, 3})
	r.Render(scene)

	px := r.Pixels()
	i := (size/2*size + size/2) * 4
	if got := [3]byte{px[i], px[i+1], px[i+2]}; got[0] < 200 || got[1] > 30 || got[2] > 30 {
		t.Errorf("plate emitting white through a red map = %v, want red", got)
	}
}
