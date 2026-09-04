package geometries

import (
	"fmt"

	"github.com/bluescreen10/pix/glm"
)

// Geometry is a ref-counted handle to a renderer-owned geometry. Clone with Copy();
// surrender ownership with Release() (the geometry is freed at refcount 0). It caches
// the local bounding sphere so scenes/loaders can frame without the geometry store.
type Geometry struct {
	ref            ref
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
func (g Geometry) Valid() bool {
	return g.ref.Valid()
}

// GetAttributeData returns a copy of a stored attribute reinterpreted as
// []T (e.g. GetAttributeData[glm.Vec3f](AttributePosition)), or nil if the attribute
// is absent. T must match the element layout the attribute was created with. Do not
// mutate the returned slice — it aliases the geometry's internal bytes.
func (g Geometry) GetAttributeData[T any](t AttributeType) []T {
	if g.store == nil {
		return nil
	}
	a := g.store.attribute(g.ref.id, t)
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
	g.store.setAttribute(g.ref.id, t, toBytes(data), len(data))
}

// BoundingSphere returns the geometry's local-space bounding sphere.
func (g Geometry) BoundingSphere() glm.Sphere {
	return g.boundingSphere
}

// id is the geometry's slot in the store (used by drawables).
func (g Geometry) ID() uint32 {
	return g.ref.id
}

// skinOutput allocates a derived output geometry that receives this geometry's
// compute-skinned vertex data (see Store.createSkinOutput), and returns a
// fresh single-ref handle to it. g must carry skin index/weight attributes.
// Package-internal — used by Scene.NewSkinnedMesh.
func (g Geometry) SkinOutput() Geometry {
	if g.store == nil {
		panic("render: SkinOutput on a geometry with no owning store")
	}
	id, gen := g.store.createSkinOutput(g.ref.id)
	return Geometry{
		ref:            newRef(id, gen, g.store.dispose, g.store.validate),
		store:          g.store,
		boundingSphere: g.store.BoundingSphere(id),
	}
}

// attribute returns a pointer to a geometry's stored attribute (nil if the id is
// dead or the attribute is absent). The generic GetAttributeData/SetAttributeData
// methods reinterpret its bytes.
func (g *Store) attribute(id uint32, t AttributeType) *Attribute {
	if !g.entries.Alive(id) || t >= attributeCount {
		return nil
	}
	e := g.entries.Get(id)
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
	if !g.entries.Alive(id) || t >= attributeCount {
		return
	}
	e := g.entries.Get(id)
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
