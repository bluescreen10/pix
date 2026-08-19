package mem_test

import (
	"testing"

	"github.com/bluescreen10/pix/internal/mem"
)

const testArenaSize uint32 = 1024 * 1024 * 256 // 256 MiB

func mustAlloc(t *testing.T, tlsf *mem.TLSF, size uint32) mem.Allocation {
	t.Helper()
	a, err := tlsf.Alloc(size)
	if err != nil {
		t.Fatalf("Alloc(%d) failed: %v", size, err)
	}
	return a
}

// allocAt allocates and asserts the resulting offset.
func allocAt(t *testing.T, tlsf *mem.TLSF, size, wantOffset uint32) mem.Allocation {
	t.Helper()
	a := mustAlloc(t, tlsf, size)
	if got := a.Offset(); got != wantOffset {
		t.Fatalf("Alloc(%d) offset = %d, want %d", size, got, wantOffset)
	}
	return a
}

func mustFree(t *testing.T, tlsf *mem.TLSF, a mem.Allocation) {
	t.Helper()
	if err := tlsf.Free(a); err != nil {
		t.Fatalf("Free failed: %v", err)
	}
}

// validateClean asserts the arena is fully coalesced: a single max-size
// allocation must succeed at offset 0.
func validateClean(t *testing.T, tlsf *mem.TLSF) {
	t.Helper()
	a := allocAt(t, tlsf, testArenaSize, 0)
	mustFree(t, tlsf, a)
}

func TestTLSFBasic(t *testing.T) {
	tlsf := mem.NewTLSF(testArenaSize)
	a := allocAt(t, tlsf, 1337, 0)
	mustFree(t, tlsf, a)
}

func TestTLSFAllocate(t *testing.T) {
	t.Run("simple", func(t *testing.T) {
		tlsf := mem.NewTLSF(testArenaSize)
		// Free merges neighbours, so a later full allocation returns offset 0.
		a := allocAt(t, tlsf, 0, 0)
		b := allocAt(t, tlsf, 1, 0)
		c := allocAt(t, tlsf, 123, 1)
		d := allocAt(t, tlsf, 1234, 124)

		mustFree(t, tlsf, a)
		mustFree(t, tlsf, b)
		mustFree(t, tlsf, c)
		mustFree(t, tlsf, d)

		validateClean(t, tlsf)
	})

	t.Run("merge trivial", func(t *testing.T) {
		tlsf := mem.NewTLSF(testArenaSize)
		a := allocAt(t, tlsf, 1337, 0)
		mustFree(t, tlsf, a)

		b := allocAt(t, tlsf, 1337, 0)
		mustFree(t, tlsf, b)

		validateClean(t, tlsf)
	})

	t.Run("reuse trivial", func(t *testing.T) {
		tlsf := mem.NewTLSF(testArenaSize)
		// C fits the same bin freed by A (pow2 size), so it reuses that node.
		a := allocAt(t, tlsf, 1024, 0)
		b := allocAt(t, tlsf, 3456, 1024)

		mustFree(t, tlsf, a)

		c := allocAt(t, tlsf, 1024, 0)

		mustFree(t, tlsf, c)
		mustFree(t, tlsf, b)

		validateClean(t, tlsf)
	})

	t.Run("reuse complex", func(t *testing.T) {
		tlsf := mem.NewTLSF(testArenaSize)
		// C doesn't fit A's freed bin, so it goes after B; D and E then reuse A's node.
		a := allocAt(t, tlsf, 1024, 0)
		b := allocAt(t, tlsf, 3456, 1024)

		mustFree(t, tlsf, a)

		c := allocAt(t, tlsf, 2345, 1024+3456)
		d := allocAt(t, tlsf, 456, 0)
		e := allocAt(t, tlsf, 512, 456)

		total, largest := tlsf.StorageReport()
		if want := testArenaSize - 3456 - 2345 - 456 - 512; total != want {
			t.Fatalf("total free = %d, want %d", total, want)
		}
		if largest == total {
			t.Fatalf("largest free region (%d) should differ from total (%d)", largest, total)
		}

		mustFree(t, tlsf, c)
		mustFree(t, tlsf, d)
		mustFree(t, tlsf, b)
		mustFree(t, tlsf, e)

		validateClean(t, tlsf)
	})

	t.Run("zero fragmentation", func(t *testing.T) {
		tlsf := mem.NewTLSF(testArenaSize)
		const mb = uint32(1024 * 1024)

		var allocs [256]mem.Allocation
		for i := uint32(0); i < 256; i++ {
			allocs[i] = allocAt(t, tlsf, mb, i*mb)
		}

		if total, largest := tlsf.StorageReport(); total != 0 || largest != 0 {
			t.Fatalf("full arena report = (%d, %d), want (0, 0)", total, largest)
		}

		// Free four random slots and four contiguous slots (must merge).
		for _, i := range []int{243, 5, 123, 95, 151, 152, 153, 154} {
			mustFree(t, tlsf, allocs[i])
		}

		// Reallocate the four random holes (1 MB each) and the merged 4 MB hole.
		for _, i := range []int{243, 5, 123, 95} {
			a, err := tlsf.Alloc(mb)
			if err != nil {
				t.Fatalf("realloc allocs[%d] failed: %v", i, err)
			}
			allocs[i] = a
		}
		if a, err := tlsf.Alloc(4 * mb); err != nil { // 4x larger, into the merged region
			t.Fatalf("realloc allocs[151] (4MB) failed: %v", err)
		} else {
			allocs[151] = a
		}

		// Free everything except the slots subsumed by the 4 MB allocation.
		for i := 0; i < 256; i++ {
			if i < 152 || i > 154 {
				mustFree(t, tlsf, allocs[i])
			}
		}

		if total, largest := tlsf.StorageReport(); total != testArenaSize || largest != testArenaSize {
			t.Fatalf("drained arena report = (%d, %d), want (%d, %d)", total, largest, testArenaSize, testArenaSize)
		}

		validateClean(t, tlsf)
	})
}
