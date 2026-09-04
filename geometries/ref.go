package geometries

import "sync/atomic"

// ref is a reference-counted, generation-stamped handle to a slab slot owned by
// this package's Store. dispose and validate are closures bound to the owning
// Store at construction (via newRef) rather than an interface: an interface's
// unexported methods must be satisfied by a type in the same package as the
// interface, which is what forced materials/textures into their own copy of this
// same shape once they too move into their own packages. Closures carry no such
// restriction, so once there's a second real user this can move into a shared
// package as-is.
type ref struct {
	id       uint32
	gen      uint32
	refCount *int32
	dispose  func(id uint32)
	validate func(id, gen uint32) bool
}

// newRef starts a fresh single-ref handle for id/gen, releasing through dispose and
// checking liveness through validate.
func newRef(id, gen uint32, dispose func(uint32), validate func(uint32, uint32) bool) ref {
	rc := int32(1)
	return ref{id: id, gen: gen, refCount: &rc, dispose: dispose, validate: validate}
}

// Copy increments the reference count and returns an additional ref to the same resource.
func (r ref) Copy() ref {
	if r.refCount != nil {
		atomic.AddInt32(r.refCount, 1)
	}
	return r
}

// Release decrements the reference count. When it reaches zero the resource is disposed.
func (r ref) Release() {
	if r.refCount == nil {
		return
	}
	if atomic.AddInt32(r.refCount, -1) == 0 {
		r.dispose(r.id)
	}
}

// Valid reports whether the underlying resource is still alive (not disposed and slot not reused).
func (r ref) Valid() bool {
	return r.validate != nil && r.validate(r.id, r.gen)
}

// ID returns the slot index into the owning resource table.
func (r ref) ID() uint32 { return r.id }
