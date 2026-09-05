package pix

import (
	"github.com/bluescreen10/pix/materials"
	"testing"
)

// TestMaterialSyncStagesManyDirtyWithoutAllocating is the point of the uploader's
// staging arena: at "tens of materials a frame" scale, sorting dirty ids to coalesce
// them into contiguous device ranges buys nothing, because every changed record is
// staged by a bump-pointer advance in a buffer the arena already owns — no matter how
// scattered their ids are. This creates enough materials to force several grows (so ids
// land all over a large store), dirties every other one (worst case for adjacency), and
// checks Sync stages them all without growing the arena.
func TestMaterialSyncStagesManyDirtyWithoutAllocating(t *testing.T) {
	r, err := NewOffscreenRenderer(16, 16)
	if err != nil {
		t.Fatal(err)
	}
	defer r.Destroy()

	const n = 64
	mats := make([]*materials.PBRMaterial, n)
	for i := range mats {
		mats[i] = r.NewPBRMaterial()
	}
	defer func() {
		for _, m := range mats {
			m.Release()
		}
	}()

	up := r.uploader
	drain := func(sync func()) {
		cmd := r.backend.Begin()
		up.Begin(cmd)
		sync()
		up.End(cmd)
		r.backend.Wait(r.backend.Submit(cmd))
	}
	drain(func() { mats[0].Pool().Sync(up) }) // the dirtying register() did above

	for i := 0; i < n; i += 2 { // scattered, not a contiguous run
		mats[i].SetRoughness(0.9)
	}

	st := mats[0].Pool()
	arenasBefore := up.ArenaCount()
	drain(func() { st.Sync(up) })
	if got := up.ArenaCount(); got != arenasBefore {
		t.Fatalf("Sync grew the staging arena from %d to %d arenas for %d scattered dirty records, want no growth",
			arenasBefore, got, n/2)
	}
}
