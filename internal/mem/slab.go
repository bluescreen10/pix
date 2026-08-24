// Package slab is a slot-stable, generation-counted free list shared by the
// engine and the gpu layer. Slots are recycled on Free; the generation
// counter prevents stale-handle (ABA) aliasing.
package mem

import "iter"

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
func NewSlab[T any]() Slab[T] {
	return Slab[T]{freeHead: invalidIdx}
}

// Alloc claims a slot and returns its (index, generation).
func (s *Slab[T]) Alloc(val T) (id, gen uint32) {
	if s.freeHead != invalidIdx {
		id = s.freeHead
		s.freeHead = s.entries[id].freeNext
		s.entries[id].val = val
		s.entries[id].alive = true
		return id, s.entries[id].gen
	}
	id = uint32(len(s.entries))
	s.entries = append(s.entries, entry[T]{val: val, gen: 1, alive: true})
	return id, 1
}

// Free marks a slot dead, bumps its generation (so existing handles become
// detectably stale), and returns it to the pool for reuse.
func (s *Slab[T]) Free(id uint32) {
	s.entries[id].gen++
	s.entries[id].alive = false
	s.entries[id].freeNext = s.freeHead
	s.freeHead = id
}

// Get returns a pointer to a slot's value. Only valid while the slot is alive.
func (s *Slab[T]) Get(id uint32) *T {
	return &s.entries[id].val
}

// Generation returns the current generation of a slot.
func (s *Slab[T]) Generation(id uint32) uint32 {
	return s.entries[id].gen
}

// Valid reports whether (id, gen) refers to a live slot.
func (s *Slab[T]) Valid(id, gen uint32) bool {
	return id < uint32(len(s.entries)) && s.entries[id].alive && s.entries[id].gen == gen
}

// Alive reports whether id refers to a live slot (regardless of generation).
func (s *Slab[T]) Alive(id uint32) bool {
	return id < uint32(len(s.entries)) && s.entries[id].alive
}

// Len returns the number of slots ever allocated (including freed ones), i.e. one
// past the largest index handed out. Parallel arrays keyed by index size to this.
func (s *Slab[T]) Len() int { return len(s.entries) }

// All iterates live slots, yielding each slot's index and a pointer to its value
// (so callers can mutate in place). Unlike Items, which yields value copies.
func (s *Slab[T]) All() iter.Seq2[uint32, *T] {
	return func(yield func(uint32, *T) bool) {
		for i := range s.entries {
			if !s.entries[i].alive {
				continue
			}
			if !yield(uint32(i), &s.entries[i].val) {
				break
			}
		}
	}
}

// Range calls fn for each live value.
func (s *Slab[T]) Items() iter.Seq[T] {
	return func(yield func(T) bool) {
		for _, e := range s.entries {
			if !e.alive {
				continue
			}

			if !yield(e.val) {
				break
			}
		}
	}
}
