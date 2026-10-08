package scenes

import (
	"iter"
	"sync/atomic"

	"github.com/bluescreen10/pix/colors"
	"github.com/bluescreen10/pix/geometries"
	"github.com/bluescreen10/pix/glm"
	"github.com/bluescreen10/pix/materials"
)

// A FramePacket is how a renderer is told what to draw, without being told what the
// thing describing it is. Scene produces one; so could an ECS, an editor, or a test
// with no scene graph at all.
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

	// Views are the images to render from these tables, composed in order — for a
	// Scene, one per attached, visible camera. A packet describes one source and may be
	// seen from several viewpoints; simulation and table uploads happen once, not once
	// per view.
	Views []ViewPacket

	// Transforms holds one world matrix per producer node. InstanceTransforms holds the
	// per-instance matrices of instanced meshes, addressed as if appended to Transforms:
	// an index of len(Transforms.Data)+k means InstanceTransforms.Data[k]. They stay two
	// tables rather than one so both can be borrowed rather than concatenated, and
	// because they are invalidated by different things — a node moving versus an
	// instance being rewritten.
	Transforms         Table[glm.Mat4f]
	InstanceTransforms Table[glm.Mat4f]
	// TransformsDirty says the matrices changed since the last extraction. A coarse
	// whole-table flag for now, where the other tables carry revisions: transforms are
	// rewritten and re-uploaded wholesale, and ranged invalidation is a later phase.
	TransformsDirty bool
	// Meshes is one entry per renderable OBJECT, not per draw record. A mesh with four
	// LOD levels drawn at a thousand instances is one entry here; expanding that into
	// draw records is the renderer's business, and how it does so is not something a
	// producer should have to model.
	Meshes Table[MeshPacket]
	// LODs holds the coarser levels of every LOD-tagged mesh, addressed by
	// MeshPacket.LODRange. Level 0 lives on the MeshPacket itself.
	LODs Table[LODLevel]
	// Lights carries light VALUES and shadow SETTINGS — never shadow maps, cameras, or
	// fitted matrices. Those are renderer-owned resources, cached per light identity;
	// a producer says "this light casts shadows at 2048px", not where the depth buffer
	// lives. Environment is the scene-wide ambient and fog, which ride in the same GPU
	// table the lights do.
	Lights      Table[LightPacket]
	Environment EnvironmentPacket

	// Skins is one entry per skinned mesh and Joints the flat palette they index. The
	// producer poses the skeleton; the renderer runs the vertex skinning and owns the
	// geometry it writes into. Joint matrices are plain CPU data here — the buffer they
	// end up in belongs to the renderer, which is why extraction computes them into a
	// slice rather than straight into device memory as it once did.
	Skins  Table[SkinPacket]
	Joints Table[glm.Mat4f]

	// Particles is one entry per simulated system, carrying both its persistent
	// description and this frame's simulation step. Newborns is the flat table the
	// steps' ranges point into.
	//
	// Particles are the one place where a packet carries work that must happen exactly
	// once. Extraction copies pending births into borrowed packet storage without
	// consuming them; Producer.Rendered retires the step only after submission.
	Particles Table[ParticlePacket]
	Newborns  Table[ParticleRecord]
}

// ParticleID identifies a particle system across frames. The renderer keys its
// simulation buffers on it, and those buffers ARE the simulation — losing the mapping
// loses the state, so an id must never be reused.
type ParticleID uint32

// ParticlePacket is one particle system's description plus its step for this frame.
type ParticlePacket struct {
	ID        ParticleID
	Transform uint32 // Slot in FramePacket.Transforms.
	Geometry  geometries.ID
	Material  materials.ID
	Capacity  uint32
	Update    ParticleUpdate
	Sort      ParticleSort

	// DT is the simulation time to advance, accumulated since the last extraction; 0
	// means no step was requested and the renderer retains the current state rather
	// than running a compaction pass for nothing.
	DT float32
	// Newborns is this step's births, a range into FramePacket.Newborns.
	Newborns IndexRange
	// Epoch increments when the system is cleared. The renderer zeroes its buffers on
	// a change, which is what makes Clear mean "start empty" rather than "keep whatever
	// happened to be in memory".
	Epoch uint64
}

// SkinPacket is one skinned mesh's dispatch description: which geometry to read, which
// to write, the joint palette to read it with, and how many vertices that is.
type SkinPacket struct {
	Source geometries.ID // Bind-pose positions, attributes and skin weights.
	Output geometries.ID // Where the skinned result is written.
	// Joints is this skeleton's palette range in FramePacket.Joints, in skeleton-local
	// space (rootWorldInv * boneWorld * invBind) — so the mesh's own draw transform is
	// the skeleton root's, and moving a character rigidly changes no joint at all.
	Joints      IndexRange
	VertexCount uint32
}

// LightID identifies a light across frames. The renderer keys its shadow resources on
// it, so it must stay stable while the light lives and never be reused after it dies —
// otherwise a new light inherits the old one's depth map mid-fit.
type LightID uint32

// LightKind selects which fields of a LightPacket mean anything.
type LightKind uint8

const (
	LightDirectional LightKind = iota // Direction only; parallel rays, no falloff.
	LightPoint                        // Position + Range; omnidirectional.
	LightSpot                         // Position + Direction + Range + Angle + Penumbra.
)

// LightPacket is one light's description. Every field is a value a producer can state
// without knowing anything about how shadows are implemented — there is deliberately
// no camera here, because fitting a shadow camera depends on the view being rendered
// and on bounds the renderer already tracks.
type LightPacket struct {
	ID   LightID
	Kind LightKind

	Position  glm.Vec3f // Point, Spot.
	Direction glm.Vec3f // Directional, Spot; the direction light travels.
	Color     colors.RGB32F
	Intensity float32
	Range     float32 // Point, Spot: distance at which falloff reaches zero.
	Angle     float32 // Spot: outer cone half-angle, radians.
	Penumbra  float32 // Spot: 0..1 fraction of the cone used for the soft edge.

	// Shadow settings, not shadow state. CastsShadow asks for shadows; whether any are
	// produced is the renderer's call — it may have no map allocated yet, or shadows
	// may be disabled globally.
	CastsShadow bool
	ShadowSize  uint32  // Requested resolution per side; 0 means the renderer's default.
	ShadowBias  float32 // Extra depth offset in WORLD units, on top of a derived term.

	// Directional: how the shadow covers the view (see DirectionalShadow). Method is
	// how the map is laid over it and Splits where each cascade but the last ends, as
	// fractions of the distance. How many cascades and how far they reach are read
	// through ShadowCascades and ShadowDistance, which fill in the defaults.
	ShadowMethod   ShadowMethod
	shadowCascades int
	shadowDistance float32
	ShadowSplits   [MaxShadowCascades - 1]float32

	// Directional: the light's mask (see LightMask), as the values a renderer needs —
	// the texture's bindless index, the world size of one repeat, and its offset. A
	// MaskSize of 0 means the light has no mask.
	MaskTexture uint32
	MaskSize    float32
	MaskOffset  glm.Vec3f
}

// ShadowCascades is how many slices a directional light's shadow is split into: the
// light's own count clamped to [1, MaxShadowCascades], or DefaultShadowCascades when it
// states none.
func (l LightPacket) ShadowCascades() int {
	if l.shadowCascades <= 0 {
		return DefaultShadowCascades
	}
	return min(l.shadowCascades, MaxShadowCascades)
}

// ShadowDistance is how far from the eye a directional light's shadow reaches, in world
// units, or DefaultShadowDistance when the light states none.
func (l LightPacket) ShadowDistance() float32 {
	if l.shadowDistance <= 0 {
		return DefaultShadowDistance
	}
	return l.shadowDistance
}

// EnvironmentPacket is the scene-wide lighting environment.
type EnvironmentPacket struct {
	Ambient colors.RGB32F
	Fog     FogState
	Map     EnvironmentMapState
}

// EnvironmentMapState is the resolved form of an Environment: the values a packet
// carries. Texture is its image's bindless index, and Revision changes whenever the
// image does — so a renderer derives the environment's light again only then. A zero
// Revision means the scene has no environment.
type EnvironmentMapState struct {
	Texture    uint32
	Revision   uint64
	Intensity  float32
	Rotation   float32
	Background bool
}

// Producer is anything that can describe a frame: a scene graph, an ECS, an editor, a
// test fixture with no world behind it at all. It is the whole of what a Renderer
// requires, and the reason the renderer package does not import a scene graph.
//
// The three methods are deliberately the smallest set that makes a frame correct.
// There is no acknowledgement token and no commit protocol — see
// docs/frame-packet.md.
type Producer interface {
	// ID is this producer's stable lifetime identity. The renderer caches GPU state
	// per source and releases it through Renderer.ReleaseSource.
	ID() SourceID

	// Extract publishes the current frame description into p, borrowing the producer's
	// own cached tables. It must not CONSUME anything: extracting without rendering has
	// to leave the producer exactly as it was, or a dropped frame silently loses work.
	Extract(p *FramePacket)

	// Rendered says the packet last extracted was submitted, so whatever that frame
	// consumed can now be retired — a particle system's queued births and accumulated
	// dt. Called by the renderer after submission, and only then.
	Rendered()
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

func (t Table[T]) Entries() iter.Seq2[int, T] {
	return func(yield func(int, T) bool) {
		for i, v := range t.Data {
			if !yield(i, v) {
				break
			}
		}
	}
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
	// ID is this object's stable identity; its position in the table is not.
	ID ObjectID
	// Transforms is the span of FramePacket.Transforms this mesh is drawn at — one
	// draw per entry. An ordinary mesh has Count == 1; an instanced mesh has one per
	// instance. Deliberately a range rather than the specification's single index:
	// instancing then needs no second table and no special case, and the renderer's
	// expansion loop is the same code for one instance as for ten thousand.
	Transforms IndexRange
	// Geometry and Material are LOD level 0 — the mesh as created, before any coarser
	// level was added.
	Geometry geometries.ID
	Material materials.ID
	// Bounds is in local space, transformed by each entry in Transforms. Every LOD
	// level shares it: a coarser level approximates the same object.
	Bounds glm.Sphere
	Flags  RenderFlags
	// LODRange spans this mesh's COARSER levels in FramePacket.LODs, in order. An
	// empty range means the mesh has only level 0, which is the common case.
	LODRange IndexRange
	// LODHysteresis widens whichever level was selected last frame, so an object
	// hovering on a boundary does not flip between levels. Unused without LOD levels.
	LODHysteresis float32
}

// LODLevel is one coarser level of a mesh's LOD chain, as appended by AddLOD. Level 0
// is not here — it lives on the MeshPacket, because every mesh has one and making it a
// range entry would mean an allocation and an indirection for the common case of a
// mesh with no LOD at all.
type LODLevel struct {
	Geometry geometries.ID
	Material materials.ID
	// MinDistance is the camera distance at which this level takes over from the
	// previous one. Levels are ordered nearest-first, so these ascend.
	MinDistance float32
}

// ViewPacket is one camera's worth of a frame. Projection uses Pix's canonical camera
// convention; the renderer applies whatever clip-space adjustment its backend needs, so
// a producer never encodes a backend assumption into a packet.
type ViewPacket struct {
	ID         ViewID
	View       glm.Mat4f
	Projection glm.Mat4f
	Position   glm.Vec3f
	// Exposure scales the light the view sees before tone mapping, in stops (see
	// Camera.SetExposure).
	Exposure float32
}

// ViewProjection returns the view's world-to-clip matrix.
func (v ViewPacket) ViewProjection() glm.Mat4f {
	return v.Projection.Mul4x4(v.View)
}
