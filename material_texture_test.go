package pix

import "testing"

// Reproduces what loaders/gltf does: one cached texture handed to two map slots.
func TestSharedTextureOwnership(t *testing.T) {
	r, _ := NewOffscreenRenderer(16, 16)
	defer r.Destroy()

	tex := r.NewTexture([]byte{255, 255, 255, 255}, 1, 1, TextureLinear)
	m := r.NewPBRMaterial()
	m.SetMetallicMap(tex)
	m.SetRoughnessMap(tex) // same handle into a second slot
	m.Release()

	if !tex.Valid() {
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
	r, err := NewOffscreenRenderer(16, 16)
	if err != nil {
		t.Fatal(err)
	}
	defer r.Destroy()

	tex := r.NewTexture([]byte{255, 255, 255, 255}, 1, 1, TextureLinear)
	m := r.NewBasicMaterial()
	m.SetColorMap(tex)
	tex.Release() // the material should hold the only remaining reference

	m.SetColorMap(m.ColorMap()) // rebind to itself

	if !m.ColorMap().Valid() {
		t.Fatal("self-rebind destroyed the texture")
	}
}

// TestSetMapSelfRebindThenReleaseIsSafe is the sharper form of the same hazard: a
// release-before-copy ordering does not just invalidate the self-rebound handle, it
// leaks its refcount to a nonzero value tied to a retired generation. Releasing the
// material later then disposes whatever texture has since been allocated into that
// same store slot — a completely unrelated resource.
func TestSetMapSelfRebindThenReleaseIsSafe(t *testing.T) {
	r, err := NewOffscreenRenderer(16, 16)
	if err != nil {
		t.Fatal(err)
	}
	defer r.Destroy()

	tex := r.NewTexture([]byte{255, 255, 255, 255}, 1, 1, TextureLinear)
	m := r.NewBasicMaterial()
	m.SetColorMap(tex)
	tex.Release()
	m.SetColorMap(m.ColorMap())

	other := r.NewTexture([]byte{0, 0, 0, 255}, 1, 1, TextureLinear)
	defer other.Release()

	m.Release()

	if !other.Valid() {
		t.Fatal("releasing the material after a self-rebind disposed an unrelated texture")
	}
}
