package pix

import (
	"testing"
)

// TestMaterialPoolIsNotSharedAcrossRenderers guards against caching a pool in a
// package var: Renderer.Destroy frees every pool's GPU buffer, so a pool cached
// across renderers (a sync.Once especially, which cannot refire) would hand the next
// renderer a pool whose buffer is gone, and alloc would fault writing it.
func TestMaterialPoolIsNotSharedAcrossRenderers(t *testing.T) {
	first, err := NewOffscreenRenderer(16, 16)
	if err != nil {
		t.Fatal(err)
	}
	m := first.NewPBRMaterial()
	stale := m.Pool()
	first.Destroy()
	if stale.RecordsAddr() != 0 {
		t.Fatal("Renderer.Destroy left the material pool's buffer allocated")
	}

	second, err := NewOffscreenRenderer(16, 16)
	if err != nil {
		t.Fatal(err)
	}
	defer second.Destroy()
	m2 := second.NewPBRMaterial()
	if m2.Pool() == stale {
		t.Fatal("second renderer was handed the destroyed pool")
	}
	m2.SetRoughness(0.25) // writes the mapped buffer; faults if the store is dead
}
