package pix

import (
	"testing"

	"github.com/bluescreen10/pix/gpu"
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
	mats := make([]*PBRMaterial, n)
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
	drain(func() { mats[0].store.Sync(up) }) // the dirtying register() did above

	for i := 0; i < n; i += 2 { // scattered, not a contiguous run
		mats[i].SetRoughness(0.9)
	}

	st := mats[0].store
	if len(st.dirty) != n/2 {
		t.Fatalf("dirty set has %d ids, want %d", len(st.dirty), n/2)
	}

	arenasBefore := len(up.arenas)
	drain(func() { st.Sync(up) })
	if got := len(up.arenas); got != arenasBefore {
		t.Fatalf("Sync grew the staging arena from %d to %d arenas for %d scattered dirty records, want no growth",
			arenasBefore, got, n/2)
	}
}

// TestUploaderArenaReusesMemoryAcrossFrames checks the property the arena exists for:
// once it has grown to a frame's high-water mark, repeating that frame allocates
// nothing. It also covers the overflow path collapsing back to a single arena — which
// happens at the NEXT Start, since that is where the arena is reclaimed.
func TestUploaderArenaReusesMemoryAcrossFrames(t *testing.T) {
	r, err := NewOffscreenRenderer(16, 16)
	if err != nil {
		t.Fatal(err)
	}
	defer r.Destroy()

	dst := r.backend.Alloc(4<<20, gpu.MemoryDevice, "test-dst")
	defer r.backend.Free(dst)

	up := r.uploader

	// A frame's worth of copies bigger than one default arena, to force overflow.
	big := make([]byte, stagingArenaMinSize/2)
	frame := func() {
		cmd := r.backend.Begin()
		up.Begin(cmd)
		for i := 0; i < 6; i++ {
			up.copy(dst, uint32(i)*uint32(len(big)), big)
		}
		up.End(cmd)
		r.backend.Wait(r.backend.Submit(cmd))
	}

	frame()
	if len(up.arenas) < 2 {
		t.Fatalf("staging %d bytes in %d-byte arenas used %d arenas, want the overflow path",
			uint64(len(big))*6, stagingArenaMinSize, len(up.arenas))
	}

	// The next frame's Start reclaims the arena and collapses it to one buffer big
	// enough for what the last frame needed — so this frame allocates nothing more.
	frame()
	if len(up.arenas) != 1 {
		t.Fatalf("arena holds %d buffers after a repeat frame, want 1 (they should collapse)", len(up.arenas))
	}
	size := up.arenas[0].buf.Size

	frame()
	if len(up.arenas) != 1 || up.arenas[0].buf.Size != size {
		t.Fatalf("repeating the same frame resized the arena (%d buffers, %d bytes; want 1 buffer, %d bytes)",
			len(up.arenas), up.arenas[0].buf.Size, size)
	}
}
