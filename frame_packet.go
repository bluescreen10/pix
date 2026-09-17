package pix

import (
	"sync/atomic"

	"github.com/bluescreen10/pix/geometries"
	"github.com/bluescreen10/pix/glm"
	"github.com/bluescreen10/pix/materials"
)

// A FramePacket is how a renderer is told what to draw, without being told what the
// thing describing it is. Scene produces one; so could an ECS, an editor, or a test
// with no scene graph at all. See docs/frame-packet.md for the full specification.
//
// It is a borrowed view of the producer's cached tables, not a copy of the world. Each
// table carries a revision, so a renderer that already saw revision N re-reads nothing;
// a renderer seeing the producer for the first time initializes from the complete Data
// slice without calling back into it. Handing over a packet is O(1) in the size of the
// scene: what crosses the boundary is a handful of slice headers.
//
// The packet holds no Node, no *Scene, no GPU handle, no command buffer, and no
// callback that would consult the producer. Resources appear as plain identities
// (geometries.ID, materials.ID), which the renderer resolves through the stores it
// owns.
type FramePacket struct {
	// Source identifies the producer that filled this packet, so a renderer can keep a
	// cache per producer and know which one a table revision belongs to.
	Source SourceID
	// Frame counts extractions from this source. Time is the producer's clock, captured
	// once per extraction: changing it updates frame constants, never per-object data.
	Frame uint64
	Time  float32

	// Views are the images to render from these tables, composed in order. A packet
	// describes one source and may be seen from several viewpoints; simulation and
	// table uploads happen once, not once per view.
	Views []ViewPacket

	// Transforms holds world matrices, indexed by MeshPacket.Transform.
	Transforms Table[glm.Mat4f]
	// Meshes is one entry per drawable.
	Meshes Table[MeshPacket]
	// Materials is the DISTINCT set of materials the meshes reference, indexed by
	// MeshPacket.Material. The indirection is what keeps a material edit from dirtying
	// the mesh table, and what makes pipeline work proportional to materials in play
	// rather than objects on screen.
	Materials Table[materials.ID]
}

// SourceID is a producer's lifetime identity. A renderer caches GPU state per source,
// so two scenes must never share one, and a scene must not silently inherit the ID of
// a destroyed one — hence the process-wide counter rather than a caller-chosen value.
type SourceID uint64

var nextSourceID atomic.Uint64

// NewSourceID mints an identity for a producer that is not a Scene. A custom producer
// calls it once, at construction, and reports the same value in every packet it fills.
func NewSourceID() SourceID {
	return SourceID(nextSourceID.Add(1))
}

// ViewID identifies a viewpoint across frames, so per-view temporal state — culling
// results, LOD hysteresis — survives the camera moving. It stays stable through
// movement and projection changes; an unrelated view takes a new ID. Shadow views must
// not reuse a camera's ID, or they overwrite its LOD history.
type ViewID uint32

// ObjectID is a producer-side identity that stays stable across frames, so persistent
// per-object state (LOD hysteresis, visibility history) survives the object moving
// within a table. A table position is NOT an identity: entries are reordered by
// rebatching and reused after deletion.
//
// Gen distinguishes a reused slot from the object that used to hold it, the same way
// it does for resource references — but this is a WORLD identity, not a device one.
// Never compare or substitute an ObjectID for a geometries.ID or materials.ID.
type ObjectID struct {
	Index uint32
	Gen   uint32
}

// IndexRange is a contiguous span within a flat table. Nested variable-sized data —
// a mesh's LOD levels, a skeleton's joints — is addressed as a range into one shared
// table, not as an allocation per object.
type IndexRange struct {
	First uint32
	Count uint32
}

// Table is one versioned array of cached producer data, borrowed for the duration of a
// frame. The synchronization contract is a three-way comparison against the revision a
// consumer last applied:
//
//   - equal to Revision: nothing to do. No inspection, no copy, no upload.
//   - equal to BaseRevision: apply Dirty and the new length.
//   - anything else, or no cache at all: consume all of Data.
//
// Data is always the complete table, even when Dirty is empty — that is what lets a
// new consumer, or one that missed a revision, initialize itself correctly without the
// producer keeping an unbounded change journal. Dirty covers every change since
// BaseRevision, including entries that moved during compaction. An empty Dirty with a
// new Revision means a pure tail shrink, never an unreported edit.
//
// No consumer clears a producer's dirty state, and two consumers may sit at different
// revisions; each keeps its own cursor and falls back to the full table when it
// cannot catch up.
type Table[T any] struct {
	Revision     uint64
	BaseRevision uint64
	Data         []T
	Dirty        []IndexRange
}

// RenderFlags are the per-drawable bits the renderer's culling and shading consume.
type RenderFlags uint32

const (
	// RenderCastsShadow is structural: the shadow cull pass filters on it.
	RenderCastsShadow RenderFlags = 1 << iota
	// RenderReceivesShadow lets a lit shader skip shadow sampling for this drawable.
	RenderReceivesShadow
)

// MeshPacket is one drawable's complete description. It is plain data — no handles, no
// interfaces, nothing that points back at the producer — and deliberately close in
// shape to the GPU drawable record, so applying a dirty range is a field-for-field
// translation rather than a second traversal of somebody's scene.
type MeshPacket struct {
	// ID is this drawable's stable identity; its position in the table is not.
	ID ObjectID
	// Transform indexes FramePacket.Transforms.
	Transform uint32
	// Geometry and Material name renderer-owned resources. Material is a slot in
	// FramePacket.Materials, NOT a material record index — go through the table.
	Geometry geometries.ID
	Material uint32
	// Bounds is in local space, transformed by Transform.
	Bounds glm.Sphere
	Flags  RenderFlags
	// LODRange spans this mesh's levels in the packet's LOD table; an empty range
	// means the mesh has no LOD levels.
	LODRange IndexRange
}

// ViewPacket is one camera's worth of a frame. Projection uses Pix's canonical camera
// convention; the renderer applies whatever clip-space adjustment its backend needs, so
// a producer never encodes a backend assumption into a packet.
type ViewPacket struct {
	ID         ViewID
	View       glm.Mat4f
	Projection glm.Mat4f
	Position   glm.Vec3f
}
