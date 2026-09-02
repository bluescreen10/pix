package pix

import "testing"

// TestStoreDedupsEqualBytesFromDistinctSlices pins the fast path's fallback: two
// Shaders holding equal SPIR-V in *different* backing arrays miss the identity check
// and must still land in one store via the hash path, or a RawMaterial built from a
// freshly-read shader would get a second store for the same program.
func TestStoreDedupsEqualBytesFromDistinctSlices(t *testing.T) {
	r, err := NewOffscreenRenderer(16, 16)
	if err != nil {
		t.Fatal(err)
	}
	defer r.Destroy()

	copied := append([]byte(nil), basicForwardSPV...)
	a := r.materials.store(Shader{Forward: basicForwardSPV}, "a")
	b := r.materials.store(Shader{Forward: copied}, "b")
	if a != b {
		t.Fatal("equal SPIR-V in distinct arrays produced two stores")
	}
	// And a genuinely different shader must NOT collide.
	if c := r.materials.store(Shader{Forward: blinnPhongForwardSPV}, "c"); c == a {
		t.Fatal("different shaders shared a store")
	}
}
