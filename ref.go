package pix

import "sync/atomic"

// Disposer is the release callback interface for renderer-owned resources.
// Unexported methods restrict implementation to this package.
type Disposer interface {
	dispose(id uint32)
	generation(id uint32) uint32
}

// Ref is a reference-counted, generation-stamped handle to a renderer resource.
// The zero value is invalid. Clone with Copy(); surrender ownership with Release().
type Ref[T any] struct {
	id       uint32
	gen      uint32
	refCount *int32
	owner    Disposer
}

// Copy increments the reference count and returns an additional Ref to the same resource.
func (r Ref[T]) Copy() Ref[T] {
	if r.refCount != nil {
		atomic.AddInt32(r.refCount, 1)
	}
	return r
}

// Release decrements the reference count. When it reaches zero the resource is disposed.
func (r Ref[T]) Release() {
	if r.refCount == nil {
		return
	}
	if atomic.AddInt32(r.refCount, -1) == 0 {
		r.owner.dispose(r.id)
	}
}

// Valid reports whether the underlying resource is still alive (not disposed and slot not reused).
func (r Ref[T]) Valid() bool {
	return r.owner != nil && r.owner.generation(r.id) == r.gen
}

// ID returns the slot index into the owning resource table.
func (r Ref[T]) ID() uint32 { return r.id }
