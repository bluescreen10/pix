package gpu

import (
	"testing"

	"github.com/bluescreen10/pix/internal/mem"
)

const testArenaSize uint32 = 1024 * 1024 * 256 // 256 MiB

// newTestAllocator builds an OffsetAllocator backed by a CPU-only Instance (no
// GPU device): Alloc/Free only touch the buffer registry and never dereference
// the raw GPU buffer, so this exercises the full allocation logic without a GPU.
func newTestAllocator(size uint32) *OffsetAllocator {
	inst := &Instance{buffers: mem.NewSlab[bufferData]()}
	a := &OffsetAllocator{instance: inst, maxSize: size}
	a.reset()
	return a
}

func mustAlloc(t *testing.T, a *OffsetAllocator, size uint32) Buffer {
	t.Helper()
	b, err := a.Alloc(size)
	if err != nil {
		t.Fatalf("Alloc(%d) failed: %v", size, err)
	}
	return b
}

func bufOffset(a *OffsetAllocator, b Buffer) uint32 {
	return uint32(a.instance.BufferOffset(b))
}

// allocAt allocates and asserts the resulting offset.
func allocAt(t *testing.T, a *OffsetAllocator, size, wantOffset uint32) Buffer {
	t.Helper()
	b := mustAlloc(t, a, size)
	if got := bufOffset(a, b); got != wantOffset {
		t.Fatalf("Alloc(%d) offset = %d, want %d", size, got, wantOffset)
	}
	return b
}

func mustFree(t *testing.T, a *OffsetAllocator, b Buffer) {
	t.Helper()
	if err := a.Free(b); err != nil {
		t.Fatalf("Free failed: %v", err)
	}
}

// validateClean asserts the arena is fully coalesced: a single max-size
// allocation must succeed at offset 0.
func validateClean(t *testing.T, a *OffsetAllocator) {
	t.Helper()
	b := allocAt(t, a, testArenaSize, 0)
	mustFree(t, a, b)
}

func TestSmallFloat(t *testing.T) {
	a := &OffsetAllocator{} // roundUp/roundDown are stateless

	t.Run("uintToFloat", func(t *testing.T) {
		// Denorms and small values (0..16 with a 3-bit mantissa) are precise.
		for i := uint32(0); i < 17; i++ {
			if up := a.roundUp(i); up != i {
				t.Errorf("roundUp(%d) = %d, want %d", i, up, i)
			}
			if down := a.roundDown(i); down != i {
				t.Errorf("roundDown(%d) = %d, want %d", i, down, i)
			}
		}

		cases := []struct{ number, up, down uint32 }{
			{17, 17, 16},
			{118, 39, 38},
			{1024, 64, 64},
			{65536, 112, 112},
			{529445, 137, 136},
			{1048575, 144, 143},
		}
		for _, c := range cases {
			if up := a.roundUp(c.number); up != c.up {
				t.Errorf("roundUp(%d) = %d, want %d", c.number, up, c.up)
			}
			if down := a.roundDown(c.number); down != c.down {
				t.Errorf("roundDown(%d) = %d, want %d", c.number, down, c.down)
			}
		}
	})
}

func TestOffsetAllocatorBasic(t *testing.T) {
	a := newTestAllocator(testArenaSize)
	b := allocAt(t, a, 1337, 0)
	mustFree(t, a, b)
}

func TestOffsetAllocatorAllocate(t *testing.T) {
	t.Run("simple", func(t *testing.T) {
		a := newTestAllocator(testArenaSize)
		// Free merges neighbours, so a later full allocation returns offset 0.
		ba := allocAt(t, a, 0, 0)
		bb := allocAt(t, a, 1, 0)
		bc := allocAt(t, a, 123, 1)
		bd := allocAt(t, a, 1234, 124)

		mustFree(t, a, ba)
		mustFree(t, a, bb)
		mustFree(t, a, bc)
		mustFree(t, a, bd)

		validateClean(t, a)
	})

	t.Run("merge trivial", func(t *testing.T) {
		a := newTestAllocator(testArenaSize)
		ba := allocAt(t, a, 1337, 0)
		mustFree(t, a, ba)

		bb := allocAt(t, a, 1337, 0)
		mustFree(t, a, bb)

		validateClean(t, a)
	})

	t.Run("reuse trivial", func(t *testing.T) {
		a := newTestAllocator(testArenaSize)
		// C fits the same bin freed by A (pow2 size), so it reuses that node.
		ba := allocAt(t, a, 1024, 0)
		bb := allocAt(t, a, 3456, 1024)

		mustFree(t, a, ba)

		bc := allocAt(t, a, 1024, 0)

		mustFree(t, a, bc)
		mustFree(t, a, bb)

		validateClean(t, a)
	})

	t.Run("reuse complex", func(t *testing.T) {
		a := newTestAllocator(testArenaSize)
		// C doesn't fit A's freed bin, so it goes after B; D and E then reuse A's node.
		ba := allocAt(t, a, 1024, 0)
		bb := allocAt(t, a, 3456, 1024)

		mustFree(t, a, ba)

		bc := allocAt(t, a, 2345, 1024+3456)
		bd := allocAt(t, a, 456, 0)
		be := allocAt(t, a, 512, 456)

		if want := testArenaSize - 3456 - 2345 - 456 - 512; a.freeSize != want {
			t.Fatalf("free space = %d, want %d", a.freeSize, want)
		}

		mustFree(t, a, bc)
		mustFree(t, a, bd)
		mustFree(t, a, bb)
		mustFree(t, a, be)

		validateClean(t, a)
	})

	t.Run("zero fragmentation", func(t *testing.T) {
		a := newTestAllocator(testArenaSize)
		const mb = uint32(1024 * 1024)

		var allocations [256]Buffer
		for i := uint32(0); i < 256; i++ {
			allocations[i] = allocAt(t, a, mb, i*mb)
		}

		if a.freeSize != 0 {
			t.Fatalf("full arena free space = %d, want 0", a.freeSize)
		}

		// Free four random slots and four contiguous slots (must merge).
		for _, i := range []int{243, 5, 123, 95, 151, 152, 153, 154} {
			mustFree(t, a, allocations[i])
		}

		// Reallocate the four random holes (1 MB each) and the merged 4 MB hole.
		for _, i := range []int{243, 5, 123, 95} {
			if b, err := a.Alloc(mb); err != nil {
				t.Fatalf("realloc allocations[%d] failed: %v", i, err)
			} else {
				allocations[i] = b
			}
		}
		if b, err := a.Alloc(4 * mb); err != nil { // 4x larger, into the merged region
			t.Fatalf("realloc allocations[151] (4MB) failed: %v", err)
		} else {
			allocations[151] = b
		}

		// Free everything except the slots subsumed by the 4 MB allocation.
		for i := 0; i < 256; i++ {
			if i < 152 || i > 154 {
				mustFree(t, a, allocations[i])
			}
		}

		if a.freeSize != testArenaSize {
			t.Fatalf("drained arena free space = %d, want %d", a.freeSize, testArenaSize)
		}

		validateClean(t, a)
	})
}
