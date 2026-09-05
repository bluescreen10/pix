// Package ref is the reference-counted, generation-stamped handle shared by the
// renderer's resource stores (geometries, textures). It is internal: a Ref is an
// implementation detail of the handle types built on it (geometries.Geometry,
// textures.Texture), never something a caller of pix constructs directly.
package ref

import "sync/atomic"

// Ref is a reference-counted, generation-stamped handle to a slot in an owning
// store's slab. The zero value is invalid. Clone with Copy(); surrender ownership
// with Release().
//
// dispose and validate are closures bound to the owning store by New, not an
// interface: an interface's unexported methods must be satisfied by a type in the
// same package as the interface, so a sealed Disposer could never be shared across
// package boundaries — every store would need its own copy of this file. Closures
// carry no such restriction, which is what lets one implementation serve every
// store.
type Ref struct {
	id       uint32
	gen      uint32
	refCount *int32
	dispose  func(id uint32)
	validate func(id, gen uint32) bool
}

// New starts a fresh single-reference handle for id/gen, releasing through dispose
// and checking liveness through validate.
func New(id, gen uint32, dispose func(uint32), validate func(id, gen uint32) bool) Ref {
	rc := int32(1)
	return Ref{id: id, gen: gen, refCount: &rc, dispose: dispose, validate: validate}
}

// Copy increments the reference count and returns an additional Ref to the same resource.
func (r Ref) Copy() Ref {
	if r.refCount != nil {
		atomic.AddInt32(r.refCount, 1)
	}
	return r
}

// Release decrements the reference count. When it reaches zero the resource is disposed.
func (r Ref) Release() {
	if r.refCount == nil {
		return
	}
	if atomic.AddInt32(r.refCount, -1) == 0 {
		r.dispose(r.id)
	}
}

// Valid reports whether the underlying resource is still alive (not disposed and slot not reused).
func (r Ref) Valid() bool {
	return r.validate != nil && r.validate(r.id, r.gen)
}

// ID returns the slot index into the owning resource table.
func (r Ref) ID() uint32 {
	return r.id
}
