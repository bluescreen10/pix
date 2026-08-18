// Package slab is a slot-stable, generation-counted free list shared by the
// engine and the gpu layer. Slots are recycled on Reclaim; the generation
// counter prevents stale-handle (ABA) aliasing.
package slab

const invalidIdx = ^uint32(0)

// entry wraps a value with free-list bookkeeping.
type entry[T any] struct {
	val      T
	gen      uint32
	freeNext uint32
	alive    bool
}

// Slab is the generation-counted slot store.
type Slab[T any] struct {
	entries  []entry[T]
	freeHead uint32
}

// New returns an empty slab.
func New[T any]() Slab[T] {
	return Slab[T]{freeHead: invalidIdx}
}

// Alloc claims a slot and returns its (index, generation).
func (s *Slab[T]) Alloc(val T) (idx, gen uint32) {
	if s.freeHead != invalidIdx {
		idx = s.freeHead
		s.freeHead = s.entries[idx].freeNext
		s.entries[idx].val = val
		s.entries[idx].alive = true
		return idx, s.entries[idx].gen
	}
	idx = uint32(len(s.entries))
	s.entries = append(s.entries, entry[T]{val: val, gen: 1, alive: true})
	return idx, 1
}

// Free marks a slot dead, bumps its generation (so existing handles become
// detectably stale), and returns it to the pool for reuse.
func (s *Slab[T]) Free(idx uint32) {
	s.entries[idx].gen++
	s.entries[idx].alive = false
	s.entries[idx].freeNext = s.freeHead
	s.freeHead = idx
}

// Get returns a pointer to a slot's value. Only valid while the slot is alive.
func (s *Slab[T]) Get(idx uint32) *T {
	return &s.entries[idx].val
}

// Generation returns the current generation of a slot.
func (s *Slab[T]) Generation(idx uint32) uint32 {
	return s.entries[idx].gen
}

// Valid reports whether (idx, gen) refers to a live slot.
func (s *Slab[T]) Valid(idx, gen uint32) bool {
	return idx < uint32(len(s.entries)) && s.entries[idx].alive && s.entries[idx].gen == gen
}

// Range calls fn for each live value.
func (s *Slab[T]) Range(fn func(v *T)) {
	for i := range s.entries {
		if s.entries[i].alive {
			fn(&s.entries[i].val)
		}
	}
}
