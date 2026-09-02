package pix

import (
	"strings"
	"testing"
)

// TestRegisterRejectsWrongRecordSize covers register's guard: a store is keyed by
// shader and its stride comes from the first material registered, so a material whose
// Bytes are a different length would write over neighbouring slots.
func TestRegisterRejectsWrongRecordSize(t *testing.T) {
	r, err := NewOffscreenRenderer(16, 16)
	if err != nil {
		t.Fatal(err)
	}
	defer r.Destroy()

	st := r.materials.store(Shader{Forward: basicForwardSPV}, "probe")
	st.register(&BasicMaterial{}) // sets the store's stride to the Basic record size

	defer func() {
		got, ok := recover().(string)
		if !ok || !strings.Contains(got, "material record is") {
			t.Fatalf("want a record-size panic, got %v", got)
		}
	}()
	st.register(&PBRMaterial{}) // a longer record; would overrun the neighbouring slot
	t.Fatal("register accepted a record of the wrong size")
}

// TestMaterialStoreIsNotSharedAcrossRenderers guards against caching a store in a
// package var: Renderer.Destroy frees every store's GPU buffer, so a store cached
// across renderers (a sync.Once especially, which cannot refire) would hand the next
// renderer a store whose buffer is gone, and alloc would fault writing it.
func TestMaterialStoreIsNotSharedAcrossRenderers(t *testing.T) {
	first, err := NewOffscreenRenderer(16, 16)
	if err != nil {
		t.Fatal(err)
	}
	m := first.NewPBRMaterial()
	stale := m.store
	first.Destroy()
	if stale.buf.Valid() {
		t.Fatal("Renderer.Destroy left the material store's buffer allocated")
	}

	second, err := NewOffscreenRenderer(16, 16)
	if err != nil {
		t.Fatal(err)
	}
	defer second.Destroy()
	m2 := second.NewPBRMaterial()
	if m2.store == stale {
		t.Fatal("second renderer was handed the destroyed store")
	}
	m2.SetRoughness(0.25) // writes the mapped buffer; faults if the store is dead
}
