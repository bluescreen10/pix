package geometries

import (
	"fmt"

	"github.com/bluescreen10/pix/glm"
	"github.com/bluescreen10/pix/ref"
)

// Geometry is a ref-counted handle to a renderer-owned geometry. Clone with Copy();
// surrender ownership with Release() (the geometry is freed at refcount 0). It caches
// the local bounding sphere so scenes/loaders can frame without the geometry store.
type Geometry struct {
	ref            ref.Ref
	store          *Store
	boundingSphere glm.Sphere
}

// Copy returns another handle to the same geometry, bumping the refcount.
func (g Geometry) Copy() Geometry {
	return Geometry{ref: g.ref.Copy(), store: g.store, boundingSphere: g.boundingSphere}
}

// Release drops this handle's reference; the geometry is freed when the last is released.
func (g Geometry) Release() {
	g.ref.Release()
}

// Valid reports whether the underlying geometry is still alive.
func (g Geometry) IsValid() bool {
	return g.ref.IsValid()
}

// AttributeData returns a copy of a stored attribute reinterpreted as
// []T (e.g. AttributeData[glm.Vec3f](AttributePosition)), or nil if the attribute
// is absent. T must match the element layout the attribute was created with. Do not
// mutate the returned slice — it aliases the geometry's internal bytes.
func (g Geometry) AttributeData[T any](t AttributeType) []T {
	if g.store == nil {
		return nil
	}
	a := g.store.attribute(g.ref.ID(), t)
	if a == nil {
		return nil
	}
	return fromBytes[T](a.data, a.count)
}

// SetAttributeData replaces a present attribute's data and re-uploads it. T and the
// element count must match what the attribute was created with (same byte length);
// this does not add attributes or resize — use Store.Create for that. Changing
// AttributePosition also recomputes bounds (note: a Geometry handle caches bounds
// from creation, so existing handles keep their old BoundingSphere).
func (g Geometry) SetAttributeData[T any](t AttributeType, data []T) {
	if g.store == nil {
		return
	}
	g.store.setAttribute(g.ref.ID(), t, toBytes(data), len(data))
}

// Indices returns the geometry's triangle index list, or nil if it is no longer
// alive. A geometry created without an explicit index list gets a generated
// 0..n-1 one, so this is never nil for a live geometry. Do not mutate the
// returned slice — it aliases the geometry's internal data, the same contract
// AttributeData carries.
func (g Geometry) Indices() []uint32 {
	if g.store == nil {
		return nil
	}
	return g.store.indices(g.ref.ID())
}

// VertexCount returns how many vertices the geometry has (0 once it is no longer
// alive).
func (g Geometry) VertexCount() int {
	if g.store == nil || !g.store.entries.IsAlive(g.ref.ID()) {
		return 0
	}
	return g.store.entries.Value(g.ref.ID()).attrs[AttributePosition].count
}

// BoundingSphere returns the geometry's local-space bounding sphere.
func (g Geometry) BoundingSphere() glm.Sphere {
	return g.boundingSphere
}

// ID names one geometry by value: its slot in the owning store, plus the slot's
// generation. Slot alone is what the GPU drawable records and what the store's
// descriptor lookups take; Gen is what keeps a reference that outlives its geometry
// from naming whichever one inherited the slot. Mirrors materials.ID — see
// docs/frame-packet.md on why resource references travel as plain data.
type ID struct {
	Slot uint32
	Gen  uint32
}

func (g Geometry) ID() ID {
	return ID{Slot: g.ref.ID(), Gen: g.ref.Gen()}
}

// CreateDeformOutput allocates a geometry that the renderer fills with this
// geometry's vertices after morphing and skinning, and returns a fresh single-ref
// handle to it. It shares this geometry's indices and has no CPU-side data of its
// own. g must carry skin attributes, morph targets, or both.
func (g Geometry) CreateDeformOutput() Geometry {
	if g.store == nil {
		panic("render: CreateDeformOutput on a geometry with no owning store")
	}
	id, gen := g.store.createDeformOutput(g.ref.ID())
	return Geometry{
		ref:            ref.New(id, gen, g.store.dispose, g.store.validate),
		store:          g.store,
		boundingSphere: g.store.BoundingSphereAt(id),
	}
}

// indices returns a geometry's stored index list (nil if the id is dead). A
// geometry created without indices has a generated 0..n-1 list by this point —
// see Store.Create.
func (g *Store) indices(id uint32) []uint32 {
	if !g.entries.IsAlive(id) {
		return nil
	}
	return g.entries.Value(id).indices
}

// attribute returns a pointer to a geometry's stored attribute (nil if the id is
// dead or the attribute is absent). The generic AttributeData/SetAttributeData
// methods reinterpret its bytes.
func (g *Store) attribute(id uint32, t AttributeType) *Attribute {
	if !g.entries.IsAlive(id) || t >= attributeCount {
		return nil
	}
	e := g.entries.Value(id)
	if !e.has(t) {
		return nil
	}
	return &e.attrs[t]
}

// setAttribute replaces a present attribute's bytes in place and re-uploads the
// affected stream. The element size and count must match the existing attribute
// (so the suballocation still fits); adding a new attribute or resizing is not
// supported — use Store.Create for that.
func (g *Store) setAttribute(id uint32, t AttributeType, data []byte, count int) {
	if !g.entries.IsAlive(id) || t >= attributeCount {
		return
	}
	e := g.entries.Value(id)
	if !e.has(t) {
		panic(fmt.Sprintf("render: SetAttributeData on absent attribute %d (use Store.Create to add it)", t))
	}
	old := &e.attrs[t]
	if count != old.count || len(data) != len(old.data) {
		panic(fmt.Sprintf("render: SetAttributeData size mismatch for attribute %d (want %d bytes / %d elems)", t, len(old.data), old.count))
	}
	old.data = data
	// Re-upload the affected stream into its existing suballocation.
	if t == AttributePosition {
		g.writeStream(streamPos, e.allocs[streamPos].Offset(), e.bytes(streamPos))
		e.boundingSphere = glm.BoundingSphereOf(e.vec3(AttributePosition))
	} else {
		g.writeStream(streamAttr, e.allocs[streamAttr].Offset(), e.bytes(streamAttr))
	}
}
