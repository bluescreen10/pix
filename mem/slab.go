package mem

import "iter"

// entry wraps a value with free-list bookkeeping.
type entry[T any] struct {
	value      T
	generation uint32
	nextFree   uint32
	alive      bool
}

// Slab stores values in stable, generation-counted slots. Freed slots are
// reused, while their incremented generations let callers reject stale handles.
// The zero value is an empty slab ready for use.
type Slab[T any] struct {
	entries []entry[T]
	// freeHead stores the first free index plus one, leaving zero as no free slot.
	freeHead uint32
}

// NewSlab returns an empty slab.
func NewSlab[T any]() Slab[T] {
	return Slab[T]{}
}

// Alloc stores value and returns its slot index and generation.
func (s *Slab[T]) Alloc(value T) (index, generation uint32) {
	if s.freeHead != 0 {
		index = s.freeHead - 1
		s.freeHead = s.entries[index].nextFree
		s.entries[index].value = value
		s.entries[index].alive = true
		return index, s.entries[index].generation
	}
	index = uint32(len(s.entries))
	s.entries = append(s.entries, entry[T]{
		value:      value,
		generation: 1,
		alive:      true,
	})
	return index, 1
}

// Free marks a slot dead, bumps its generation (so existing handles become
// detectably stale), and returns it to the pool for reuse.
// It panics if index is outside the slab.
func (s *Slab[T]) Free(index uint32) {
	if !s.IsAlive(index) {
		panic("mem: free of invalid slab index")
	}
	s.entries[index].generation++
	s.entries[index].alive = false
	s.entries[index].nextFree = s.freeHead
	s.freeHead = index + 1
}

// Value returns a pointer to the value at index. The pointer is valid until a
// subsequent allocation grows the slab. Value panics if index is not live.
func (s *Slab[T]) Value(index uint32) *T {
	if !s.IsAlive(index) {
		panic("mem: value of invalid slab index")
	}
	return &s.entries[index].value
}

// Generation returns the current generation of a slot.
// It panics if index is outside the slab.
func (s *Slab[T]) Generation(index uint32) uint32 {
	return s.entries[index].generation
}

// IsValid reports whether index and generation identify a live slot.
func (s *Slab[T]) IsValid(index, generation uint32) bool {
	return index < uint32(len(s.entries)) &&
		s.entries[index].alive &&
		s.entries[index].generation == generation
}

// IsAlive reports whether index identifies a live slot, regardless of generation.
func (s *Slab[T]) IsAlive(index uint32) bool {
	return index < uint32(len(s.entries)) && s.entries[index].alive
}

// Len returns the number of slots ever allocated (including freed ones), i.e. one
// past the largest index handed out. Parallel arrays keyed by index size to this.
func (s *Slab[T]) Len() int {
	return len(s.entries)
}

// Entries iterates over live slots, yielding each index and a pointer to its
// value. The pointers follow the same lifetime rules as Value.
func (s *Slab[T]) Entries() iter.Seq2[uint32, *T] {
	return func(yield func(uint32, *T) bool) {
		for i := range s.entries {
			if !s.entries[i].alive {
				continue
			}
			if !yield(uint32(i), &s.entries[i].value) {
				break
			}
		}
	}
}

// Values iterates over copies of the values in live slots.
func (s *Slab[T]) Values() iter.Seq[T] {
	return func(yield func(T) bool) {
		for _, entry := range s.entries {
			if !entry.alive {
				continue
			}

			if !yield(entry.value) {
				break
			}
		}
	}
}
