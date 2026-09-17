// Package ref is the reference-counted, generation-stamped handle shared by the
// renderer's resource stores (geometries, textures, material pools). Most callers
// never name a Ref: it is the machinery inside the handle types built on it
// (geometries.Geometry, textures.Texture), and those are what application code holds.
//
// It is public rather than internal for one reason: materials.Pool.Create hands a Ref
// back to whoever is defining a material type, and materials.Material is deliberately
// open. While this package was internal, only types inside this module could receive
// that Ref, so a material type "defined outside the materials package" could not
// actually live outside pix — the extensibility the interface promises stopped at the
// module boundary. Constructing a Ref directly is still not something to do; obtain
// one from the store that owns the resource.
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
func (r Ref) IsValid() bool {
	return r.validate != nil && r.validate(r.id, r.gen)
}

// ID returns the slot index into the owning resource table.
func (r Ref) ID() uint32 {
	return r.id
}

// Gen returns the slot's generation stamp. A slot is reused after its last handle is
// released, so ID alone cannot tell a live resource from one that merely inherited its
// place: (ID, Gen) is the identity that survives reuse. Callers that pass a resource
// reference by value — the frame packet's GeometryID/MaterialID — must carry both.
func (r Ref) Gen() uint32 {
	return r.gen
}
