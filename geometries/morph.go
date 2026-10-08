package geometries

import (
	"fmt"

	"github.com/bluescreen10/pix/glm"
	"github.com/chewxy/math32"
)

// MorphTarget is one named shape a geometry can blend toward ("smile", "blink"):
// per-vertex offsets from the base geometry, applied in proportion to a mesh's weight
// for the target. Each present slice has one entry per vertex; a nil slice means the
// target leaves that attribute alone. The slices are retained without copying, so
// their backing arrays must outlive the geometry — the same contract NewAttribute has.
//
// TangentDeltas are stored for CPU queries but not uploaded: nothing renders tangents
// yet.
type MorphTarget struct {
	Name           string
	PositionDeltas []glm.Vec3f
	NormalDeltas   []glm.Vec3f
	TangentDeltas  []glm.Vec3f
}

// morphTargetEntry is a stored MorphTarget plus what the store derives from it once.
// maxDisplacement is the farthest any vertex moves at weight 1, which is what lets a
// mesh stretch its bounds to the current weights without walking the vertices.
type morphTargetEntry struct {
	target          MorphTarget
	maxDisplacement float32
}

// vertexMorph is one vertex's offsets for one target in the morph stream (16 bytes):
// the position delta as floats, and the normal delta packed by packNormalDelta.
// A geometry's targets are stored one after another, so vertex v of target t is
// record t*vertexCount + v.
type vertexMorph struct {
	position glm.Vec3f
	normal   uint32
}

// MorphTargetCount returns how many morph targets the geometry has.
func (g Geometry) MorphTargetCount() int {
	if g.store == nil {
		return 0
	}
	return len(g.store.morphTargets(g.ref.ID()))
}

// MorphTarget returns the target at index, as it was given to Store.Create. Do not
// mutate its slices — they alias the geometry's internal data.
func (g Geometry) MorphTarget(index int) MorphTarget {
	return g.store.morphTargets(g.ref.ID())[index].target
}

// MorphTargetIndex returns the index of the first target called name, and false if
// there is none.
func (g Geometry) MorphTargetIndex(name string) (int, bool) {
	if g.store == nil {
		return 0, false
	}
	for i, m := range g.store.morphTargets(g.ref.ID()) {
		if m.target.Name == name {
			return i, true
		}
	}
	return 0, false
}

// MorphTargetMaxDisplacement returns the farthest any vertex moves when the target at
// index is applied at weight 1. A weight w moves no vertex farther than |w| times it.
func (g Geometry) MorphTargetMaxDisplacement(index int) float32 {
	return g.store.morphTargets(g.ref.ID())[index].maxDisplacement
}

// morphTargets returns a geometry's stored targets (nil if the id is dead).
func (g *Store) morphTargets(id uint32) []morphTargetEntry {
	if !g.entries.IsAlive(id) {
		return nil
	}
	return g.entries.Value(id).morphTargets
}

// newMorphTargetEntries checks targets against the geometry's vertex count and
// derives each one's maximum displacement.
func newMorphTargetEntries(targets []MorphTarget, vertexCount int) []morphTargetEntry {
	if len(targets) == 0 {
		return nil
	}
	entries := make([]morphTargetEntry, len(targets))
	for i, t := range targets {
		for _, deltas := range [][]glm.Vec3f{t.PositionDeltas, t.NormalDeltas, t.TangentDeltas} {
			if deltas != nil && len(deltas) != vertexCount {
				panic(fmt.Sprintf("render: morph target %d (%q) has %d deltas for %d vertices", i, t.Name, len(deltas), vertexCount))
			}
		}
		var maxDisplacement float32
		for _, d := range t.PositionDeltas {
			maxDisplacement = max(maxDisplacement, d.Length())
		}
		entries[i] = morphTargetEntry{target: t, maxDisplacement: maxDisplacement}
	}
	return entries
}

// packMorphTargets builds the morph stream payload: every target's records, target
// after target.
func (e *entry) packMorphTargets() []byte {
	if len(e.morphTargets) == 0 {
		return nil
	}
	n := e.attrs[AttributePosition].count
	records := make([]vertexMorph, n*len(e.morphTargets))
	for t, m := range e.morphTargets {
		targetRecords := records[t*n : (t+1)*n]
		for v := range targetRecords {
			if m.target.PositionDeltas != nil {
				targetRecords[v].position = m.target.PositionDeltas[v]
			}
			if m.target.NormalDeltas != nil {
				targetRecords[v].normal = packNormalDelta(m.target.NormalDeltas[v])
			}
		}
	}
	return toBytes(records)
}

// packNormalDelta packs a normal delta into three signed 10-bit fields (the first in
// the low bits). A delta between two unit normals has components in [-2,2], stored
// as round(c/2 * 511). Signed rather than unsigned like the base normal's encoding so
// that a zero delta stays exactly zero: weights sum many deltas, and a biased zero
// would drift every normal the target does not touch.
func packNormalDelta(d glm.Vec3f) uint32 {
	var packed uint32
	for i, c := range d {
		q := int32(math32.Round(min(max(c/2, -1), 1) * 511))
		packed |= (uint32(q) & 0x3FF) << (10 * i)
	}
	return packed
}
