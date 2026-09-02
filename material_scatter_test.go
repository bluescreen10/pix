package pix

import "testing"

// TestMaterialSyncOneStagingAllocForManyDirty is the point of scatterCopy: at "tens of
// materials a frame" scale, sorting dirty ids to coalesce them into contiguous device
// ranges buys nothing, because scatterCopy already stages every dirty record into ONE
// buffer no matter how scattered their ids are. This creates enough materials to force
// several grows (so ids land all over a large store), dirties every other one (worst
// case for adjacency), and checks Sync still does exactly one host allocation.
func TestMaterialSyncOneStagingAllocForManyDirty(t *testing.T) {
	r, err := NewOffscreenRenderer(16, 16)
	if err != nil {
		t.Fatal(err)
	}
	defer r.Destroy()

	const n = 64
	mats := make([]*PBRMaterial, n)
	for i := range mats {
		mats[i] = r.NewPBRMaterial()
	}
	defer func() {
		for _, m := range mats {
			m.Release()
		}
	}()

	up := newUploader(r.backend)
	mats[0].store.Sync(up)
	up.flush() // drain the dirtying register() did above, isolating what follows

	for i := 0; i < n; i += 2 { // scattered, not a contiguous run
		mats[i].SetRoughness(0.9)
	}

	st := mats[0].store
	if len(st.dirty) != n/2 {
		t.Fatalf("dirty set has %d ids, want %d", len(st.dirty), n/2)
	}

	st.Sync(up)
	if len(up.staging) != 1 {
		t.Fatalf("Sync staged %d buffers for %d scattered dirty records, want 1", len(up.staging), n/2)
	}
	up.flush()
}
