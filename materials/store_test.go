package materials

import (
	"testing"

	"github.com/bluescreen10/pix/shaders"
)

// TestStoreDedupsEqualBytesFromDistinctSlices pins the fast path's fallback: two
// Shaders holding equal SPIR-V in *different* backing arrays miss the identity check
// and must still land in one store via the hash path, or a RawMaterial built from a
// freshly-read shader would get a second store for the same program.
func TestStoreDedupsEqualBytesFromDistinctSlices(t *testing.T) {
	store, _ := testStore(t)

	copied := append([]byte(nil), shaders.BasicForward...)
	a := store.Pool(Shader{Forward: shaders.BasicForward}, "a")
	b := store.Pool(Shader{Forward: copied}, "b")
	if a != b {
		t.Fatal("equal SPIR-V in distinct arrays produced two stores")
	}
	// And a genuinely different shader must NOT collide.
	if c := store.Pool(Shader{Forward: shaders.BlinnPhongForward}, "c"); c == a {
		t.Fatal("different shaders shared a store")
	}
}
