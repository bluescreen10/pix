package pix

// slabEntry wraps a value with free-list bookkeeping.
type slabEntry[T any] struct {
	val      T
	gen      uint32
	freeNext uint32
	alive    bool
}

// slab is a slot-stable, generation-counted free list.
// Slots are recycled after drain; the generation counter prevents ABA aliasing.
type slab[T any] struct {
	entries  []slabEntry[T]
	freeHead uint32
}

func newSlab[T any]() slab[T] {
	return slab[T]{freeHead: invalidIdx}
}

// alloc claims a slot and returns its (index, generation).
func (s *slab[T]) alloc(val T) (idx, gen uint32) {
	if s.freeHead != invalidIdx {
		idx = s.freeHead
		s.freeHead = s.entries[idx].freeNext
		s.entries[idx].val = val
		s.entries[idx].alive = true
		return idx, s.entries[idx].gen
	}
	idx = uint32(len(s.entries))
	s.entries = append(s.entries, slabEntry[T]{val: val, gen: 1, alive: true})
	return idx, 1
}

// free marks a slot dead and bumps its generation. The slot is NOT yet
// available for reuse; call reclaim after GPU-side cleanup.
func (s *slab[T]) free(idx uint32) {
	s.entries[idx].gen++
	s.entries[idx].alive = false
}

// reclaim returns a freed slot to the pool. Call after GPU resources are released.
func (s *slab[T]) reclaim(idx uint32) {
	s.entries[idx].freeNext = s.freeHead
	s.freeHead = idx
}

// get returns a pointer to a slot's value. Only valid while the slot is alive.
func (s *slab[T]) get(idx uint32) *T {
	return &s.entries[idx].val
}

// generation returns the current generation of a slot.
func (s *slab[T]) generation(idx uint32) uint32 {
	return s.entries[idx].gen
}
