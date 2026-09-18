package mem_test

import (
	"slices"
	"testing"

	"github.com/bluescreen10/pix/mem"
)

func TestSlabZeroValueAndReuse(t *testing.T) {
	var slab mem.Slab[string]
	firstIndex, firstGeneration := slab.Alloc("first")
	secondIndex, secondGeneration := slab.Alloc("second")

	if firstIndex != 0 || firstGeneration != 1 {
		t.Fatalf("first Alloc() = (%d, %d), want (0, 1)", firstIndex, firstGeneration)
	}
	if secondIndex != 1 || secondGeneration != 1 {
		t.Fatalf("second Alloc() = (%d, %d), want (1, 1)", secondIndex, secondGeneration)
	}
	if got := slab.Len(); got != 2 {
		t.Errorf("Len() = %d, want 2", got)
	}
	if !slab.IsValid(firstIndex, firstGeneration) || !slab.IsAlive(firstIndex) {
		t.Error("first allocation is not live and valid")
	}

	slab.Free(firstIndex)
	if slab.IsValid(firstIndex, firstGeneration) || slab.IsAlive(firstIndex) {
		t.Error("freed allocation remains live or valid")
	}

	reusedIndex, reusedGeneration := slab.Alloc("reused")
	if reusedIndex != firstIndex || reusedGeneration != firstGeneration+1 {
		t.Errorf(
			"reused Alloc() = (%d, %d), want (%d, %d)",
			reusedIndex,
			reusedGeneration,
			firstIndex,
			firstGeneration+1,
		)
	}
	if slab.IsValid(firstIndex, firstGeneration) {
		t.Error("stale generation became valid after slot reuse")
	}
	if got := *slab.Value(reusedIndex); got != "reused" {
		t.Errorf("Value() = %q, want %q", got, "reused")
	}
}

func TestSlabIterators(t *testing.T) {
	slab := mem.NewSlab[int]()
	first, _ := slab.Alloc(10)
	second, _ := slab.Alloc(20)
	third, _ := slab.Alloc(30)
	slab.Free(second)

	for index, value := range slab.Entries() {
		*value += int(index)
	}

	if got, want := slices.Collect(slab.Values()), []int{10, 32}; !slices.Equal(got, want) {
		t.Errorf("Values() = %v, want %v", got, want)
	}
	if got := *slab.Value(first); got != 10 {
		t.Errorf("first Value() = %d, want 10", got)
	}
	if got := *slab.Value(third); got != 32 {
		t.Errorf("third Value() = %d, want 32", got)
	}
}

func TestSlabRejectsInvalidAccess(t *testing.T) {
	slab := mem.NewSlab[int]()
	index, _ := slab.Alloc(1)
	slab.Free(index)

	assertPanics(t, "Value on freed index", func() {
		slab.Value(index)
	})
	assertPanics(t, "double free", func() {
		slab.Free(index)
	})
}

func assertPanics(t *testing.T, name string, function func()) {
	t.Helper()
	t.Run(name, func(t *testing.T) {
		defer func() {
			if recover() == nil {
				t.Error("function did not panic")
			}
		}()
		function()
	})
}
