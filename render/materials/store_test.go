package materials_test

import (
	"testing"

	"github.com/bluescreen10/pix/render/materials"
	"github.com/bluescreen10/pix/render/shaders"
)

// TestStoreDedupsEqualBytesFromDistinctSlices pins the fast path's fallback: two
// Shaders holding equal SPIR-V in *different* backing arrays miss the identity check
// and must still land in one store via the hash path, or a RawMaterial built from a
// freshly-read shader would get a second store for the same program.
func TestStoreDedupsEqualBytesFromDistinctSlices(t *testing.T) {
	store, _ := testStore(t)

	copied := append([]byte(nil), shaders.BasicFragment...)
	a := store.Pool(materials.Shader{Fragment: shaders.BasicFragment}, "a")
	b := store.Pool(materials.Shader{Fragment: copied}, "b")
	if a != b {
		t.Fatal("equal SPIR-V in distinct arrays produced two stores")
	}
	// And a genuinely different shader must NOT collide.
	if c := store.Pool(materials.Shader{Fragment: shaders.BlinnPhongFragment}, "c"); c == a {
		t.Fatal("different shaders shared a store")
	}
}

// TestPoolLabelIsTheFirstCallersLabel pins what a pool is called: the label it was
// created with. A later caller sharing its shader gets the same pool, and its label
// names nothing.
func TestPoolLabelIsTheFirstCallersLabel(t *testing.T) {
	store, _ := testStore(t)

	store.Pool(materials.Shader{Fragment: shaders.BasicFragment}, "first")
	pool := store.Pool(materials.Shader{Fragment: shaders.BasicFragment}, "second")
	if got, want := pool.Label(), "first"; got != want {
		t.Errorf("Label() = %q, want %q", got, want)
	}
}
