package geometries

import (
	"fmt"

	"github.com/bluescreen10/pix/glm"
	"github.com/bluescreen10/pix/ref"
)

// CreateLOD returns a coarser level of detail of g: a geometry that draws g's own
// vertices with only the triangles in indices. No vertex is copied — the level reads
// g's — so it is as cheap as its index list, and anything that changes g's vertices
// changes the level's too: a mesh morphing or skinning g draws the level morphed or
// skinned for free (see SharesVerticesWith).
//
// The level keeps g's vertices alive: releasing g while the level is in use is safe.
// Vertex data — attributes, morph targets, the vertex count — is g's; the index list
// and the bounding sphere, over the vertices it uses, are the level's own. A level of
// a level reads the same vertices, from the geometry that owns them.
func (g Geometry) CreateLOD(indices []uint32) Geometry {
	if g.store == nil {
		panic("render: CreateLOD on a geometry with no owning store")
	}
	id, gen := g.store.createLOD(g, indices)
	return Geometry{
		ref:            ref.New(id, gen, g.store.dispose, g.store.validate),
		store:          g.store,
		boundingSphere: g.store.BoundingSphereAt(id),
	}
}

// SharesVerticesWith reports whether g and other draw the same vertices: one is a
// level of detail of the other (see CreateLOD), both are levels of the same geometry,
// or they are the same geometry.
func (g Geometry) SharesVerticesWith(other Geometry) bool {
	if g.store == nil || g.store != other.store {
		return false
	}
	return g.store.vertexOwner(g.ref.ID()) == other.store.vertexOwner(other.ref.ID())
}

// IsSkinned reports whether g's vertices carry skin indices and weights.
func (g Geometry) IsSkinned() bool {
	if g.store == nil || !g.store.entries.IsAlive(g.ref.ID()) {
		return false
	}
	return g.store.vertexEntry(g.ref.ID()).hasSkin()
}

// vertexOwner returns the slot of the geometry whose vertices id draws: its own,
// unless id is a level of detail.
func (g *Store) vertexOwner(id uint32) uint32 {
	if !g.entries.IsAlive(id) {
		return invalidSlot
	}
	if e := g.entries.Value(id); e.sharesVertices() {
		return e.vertexOwner.ref.ID()
	}
	return id
}

// vertexEntry returns the entry holding id's vertex data: id's own, or its owner's if
// id is a level of detail. id must be alive.
func (g *Store) vertexEntry(id uint32) *entry {
	e := g.entries.Value(id)
	if e.sharesVertices() {
		return g.entries.Value(e.vertexOwner.ref.ID())
	}
	return e
}

// invalidSlot names no geometry; vertexOwner returns it for a dead id.
const invalidSlot = ^uint32(0)

// createLOD allocates a level of detail of owner: its own index range, and a
// descriptor that copies owner's vertex bases and flags. It holds a reference to the
// geometry owning the vertices — owner's own owner, if owner is itself a level — so
// the vertices outlive every level reading them. The reference is a Copy of a handle
// the caller holds: counts are kept per handle family, so a fresh handle to the same
// slot would count on its own and keep nothing alive.
func (g *Store) createLOD(owner Geometry, indices []uint32) (id, gen uint32) {
	if !g.entries.IsAlive(owner.ref.ID()) {
		panic("render: CreateLOD on a dead geometry")
	}
	if e := g.entries.Value(owner.ref.ID()); e.sharesVertices() {
		owner = e.vertexOwner
	}
	vertexOwnerID := owner.ref.ID()
	vertices := g.entries.Value(vertexOwnerID)
	vertexCount := vertices.attrs[AttributePosition].count
	for i, index := range indices {
		if int(index) >= vertexCount {
			panic(fmt.Sprintf("render: CreateLOD index %d is %d, past the geometry's %d vertices", i, index, vertexCount))
		}
	}

	var e entry
	e.vertexOwner = owner.Copy()
	e.indices = indices
	e.boundingSphere = g.boundsOfLOD(vertexOwnerID, indices)

	d := g.descs[vertexOwnerID]
	d.IndexCount = uint32(len(indices))
	alloc := g.allocIn(streamIndex, uint32(len(indices))*streamElemSize[streamIndex])
	e.allocs[streamIndex] = alloc
	g.writeStream(streamIndex, alloc.Offset(), toBytes(indices))
	d.setBase(streamIndex, alloc.Offset()/streamElemSize[streamIndex])

	id, gen = g.entries.Alloc(e)
	if g.entries.Len() >= len(g.descs) {
		g.descs = append(g.descs, geometryDesc{})
	}
	g.descs[id] = d
	g.descDirty = true
	return id, gen
}

// boundsOfLOD is the bounding sphere of the vertices a level's indices use. A deform
// output has no CPU positions to measure, so its levels take its own bounds.
func (g *Store) boundsOfLOD(vertexOwnerID uint32, indices []uint32) glm.Sphere {
	vertices := g.entries.Value(vertexOwnerID)
	if vertices.derived || len(indices) == 0 {
		return vertices.boundingSphere
	}
	positions := vertices.vec3(AttributePosition)
	used := make([]glm.Vec3f, len(indices))
	for i, index := range indices {
		used[i] = positions[index]
	}
	return glm.BoundingSphereOf(used)
}

// refreshSharedRanges copies every borrowed range from its owner again: a level of
// detail's vertex bases, and a deform output's index range. A stream grow moves every
// geometry; a borrower holds copies of its owner's bases, which would otherwise keep
// pointing where the data used to be.
func (g *Store) refreshSharedRanges() {
	for id, e := range g.entries.Entries() {
		d := &g.descs[id]
		if e.sharesVertices() {
			owner := g.descs[e.vertexOwner.ref.ID()]
			d.PositionBase = owner.PositionBase
			d.AttributeBase = owner.AttributeBase
			d.SkinBase = owner.SkinBase
			d.MorphBase = owner.MorphBase
		}
		if e.indexOwner.store != nil {
			d.IndexBase = g.descs[e.indexOwner.ref.ID()].IndexBase
		}
	}
}
