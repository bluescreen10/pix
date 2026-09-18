// Package scenes is a scene graph: a hierarchy of nodes with transforms, meshes,
// instanced and skinned meshes, lights, particle systems and decals.
//
// It owns no GPU state and imports no renderer. What a renderer needs is published by
// Scene.Extract as a FramePacket of plain data, and everything derived from that —
// buffers, pipelines, shadow maps, simulation state — belongs to whoever consumes it.
// A Scene is therefore one possible producer of frames rather than a required one; the
// Producer interface in packet.go is the whole contract, and an ECS of your own can
// satisfy it without any of this package's types.
package scenes

import (
	"time"

	"github.com/bluescreen10/pix/colors"
	"github.com/bluescreen10/pix/glm"
	"github.com/bluescreen10/pix/internal/mem"
	"github.com/bluescreen10/pix/materials"
)

const invalidIdx = ^uint32(0)

// NodeKind tags a node's payload table.
type NodeKind uint8

const (
	KindGroup NodeKind = iota
	KindMesh
	KindInstancedMesh
	KindInstance
	KindBone
	KindSkeleton
	KindSkinnedMesh
	KindAmbientLight
	KindDirectionalLight
	KindSpotLight
	KindPointLight
	KindParticleContainer
)

// NodeFlags is the per-node flag bitset.
type NodeFlags uint32

const (
	flagAlive NodeFlags = 1 << iota
	flagCastShadow
	flagReceiveShadow
	flagDirty
	flagLocalVisible
	flagVisibleDirty
	flagVisible
	// flagAttached marks a node reachable from the scene root. Maintained by
	// flushTopoIfDirty, which already computes exactly that set. Only attached
	// nodes get their world matrix updated, so only attached meshes may draw —
	// see collectDrawables.
	flagAttached
)

// NodeID is a generation-counted handle. Zero value is invalid (gen starts at 1).
type NodeID struct {
	index uint32
	gen   uint32
}

func (id NodeID) isValid() bool {
	return id.gen != 0
}

// Scene owns the node scene graph (flat parallel arrays, linked-list hierarchy), the
// per-node transforms, the mesh/skin/particle payloads (which hold ref-counted handles
// to renderer-owned geometry and materials), and the scene lights.
//
// It owns no GPU state and holds no backend. What a renderer needs is published by
// Extract as a FramePacket of plain data; everything derived from that — buffers,
// pipelines, shadow maps, simulation state — belongs to the renderer, cached per
// Scene.ID. A Scene is therefore one possible producer rather than a required one.
type Scene struct {
	// sourceID is this scene's identity as a packet producer, minted at construction
	// and never reused. A renderer keys its per-source GPU cache on it, so a destroyed
	// scene must not hand its identity to the next one — see FramePacket.Source.
	sourceID SourceID

	parents       []NodeID
	firstChildren []NodeID
	lastChildren  []NodeID
	nextSiblings  []NodeID
	prevSiblings  []NodeID

	local []glm.Mat4f
	world []glm.Mat4f

	transforms []Transform

	flags      []NodeFlags
	generation []uint32
	kind       []NodeKind
	payload    []uint32
	// names is optional, per-node: empty ("") for a node nobody named. Set by a
	// loader from the source asset (see loaders/gltf) or by a caller via
	// Node.SetName; not otherwise used or required by anything in the scene graph.
	names []string

	freeHead uint32

	topoOrder []uint32
	topoDirty bool
	// topoPos is topoOrder's reverse index: topoPos[idx] is idx's own position in
	// topoOrder, or invalidIdx if idx has no live entry there (never attached, or
	// its entry was tombstoned — see detachFromParent/reparent's fast paths).
	// Rebuilt in full by flushTopoIfDirty, so it stays authoritative even across
	// fast-path appends that a later full rebuild supersedes.
	topoPos []uint32
	// topoHoles counts tombstoned (invalidIdx) entries currently sitting in
	// topoOrder, put there by detachFromParent's fast path instead of a full
	// rebuild. Bounded to under half of len(topoOrder) — see detachFromParent —
	// so a scene that only ever attaches/detaches leaves never grows topoOrder
	// unboundedly full of dead entries.
	topoHoles int
	root      NodeID

	meshes []meshData

	instancedMeshes []instancedMeshData
	// instanceTransforms is a flat, append-only array of per-instance transforms for
	// every InstancedMesh, separate from s.world (which is one-slot-per-scene-node and
	// tied to updateTransforms's hierarchy walk — instances have none). Addressed in
	// drawable transformIDs as if it sits right after s.world — see collectDrawables
	// and drawList.sync (drawlist.go), which uploads them into one contiguous buffer.
	instanceTransforms []glm.Mat4f

	particleContainers []particleData
	// nextParticleID mints stable per-system identities; never reused, because the
	// renderer's buffers keyed on one ARE that system's simulation state.
	nextParticleID ParticleID
	// The cached particle tables Extract publishes. Newborns are DRAINED into
	// packetNewborns each extraction rather than borrowed — a simulation step has to
	// happen exactly once.
	packetParticles []ParticlePacket
	packetNewborns  []ParticleRecord

	// Skinning: skeletons (bone hierarchies + inverse binds) and skinned meshes
	// (a source geometry + a compute-derived output geometry, bound to a skeleton).
	// Both use a Slab rather than swap-remove: skinnedMeshData.skeleton stores a
	// skeletons slab id, which a swap-remove would silently invalidate.
	skeletons     mem.Slab[skeletonData]
	skinnedMeshes mem.Slab[skinnedMeshData]

	// packetJoints is every skeleton's current joint palette, laid out one skeleton
	// after another and rewritten each Sync. Published in the packet; the buffer it is
	// uploaded into belongs to the renderer.
	packetJoints []glm.Mat4f
	// packetSkins is the cached skin table Extract publishes.
	packetSkins []SkinPacket

	// Light objects the scene owns; the flat GPU table (lights) is derived from them
	// (+ ambient) each frame in Sync.
	ambient     colors.RGB32F
	fog         Fog
	dirLights   []*DirectionalLight
	pointLights []*PointLight
	spotLights  []*SpotLight
	// nextLightID mints stable per-light identities. Never reused, so a renderer's
	// shadow resources can be keyed on one without a dead light's map being inherited.
	nextLightID LightID
	// packetLights is the cached light table Extract publishes, refilled each
	// extraction: lights are a handful of mutable value objects, so polling them is
	// cheaper than tracking dirtiness (see docs/frame-packet.md).
	packetLights []LightPacket

	drawableDirty bool

	// The cached rendering description Extract publishes, rebuilt only when the scene
	// changes structurally (drawableDirty) and borrowed by every packet in between —
	// which is what makes extracting an unchanged scene cost slice headers rather than
	// a walk. See extract.go.
	//
	// packetMaterials is the DISTINCT set of materials the meshes reference; a mesh
	// names one by its slot here. The indirection is what keeps a material edit from
	// dirtying the mesh table, and what makes the renderer's pipeline work proportional
	// to materials in play rather than objects on screen.
	packetMeshes    []MeshPacket
	packetLODs      []LODLevel
	packetMaterials []materials.ID
	// meshRevision advances whenever the three tables above are rebuilt, so a consumer
	// can tell at a glance whether anything it cached is stale.
	meshRevision uint64
	// matSlot is the rebuild's dedup scratch, keyed by material identity and reused
	// across rebuilds so the walk does not allocate a map every time.
	matSlot map[materials.ID]uint32

	// transformsDirty says the world matrices changed since the last extraction, so
	// whoever uploads them knows to. The scene no longer uploads them itself: the
	// buffer they land in is the renderer's.
	transformsDirty bool

	// FrameSphere scratch, retained because it runs every frame (see prepareShadows).
	frameCenters []glm.Vec3f
	frameReach   []float32
	frameScratch []float32

	// clockStart anchors elapsed (below), recomputed each Sync — set once here so
	// every draw/particle root's time field (drawable.go, particle.go) shares one
	// scene-wide clock without each caller inventing its own.
	clockStart time.Time
	elapsed    float32
}

// New creates an empty scene.
//
// It takes no backend, and holds none: a Scene is a description of what to draw, and
// every GPU object that used to hang off it — the draw list, the light table, the joint
// palette, the particle simulation buffers — now belongs to whichever Renderer consumes
// its packets. That is what lets scene-graph and transform tests run with no GPU at
// all, and what makes Scene replaceable by an ECS of your own: see Extract.
func New() *Scene {
	s := &Scene{
		freeHead: invalidIdx, topoDirty: true,
		clockStart: time.Now(), sourceID: NewSourceID(),
	}
	s.skeletons = mem.NewSlab[skeletonData]()
	s.skinnedMeshes = mem.NewSlab[skinnedMeshData]()
	s.root = s.allocNode(KindGroup)
	s.flags[s.root.index] = flagAlive | flagLocalVisible | flagVisible
	return s
}

// ID is the scene's identity as a packet producer. A renderer caches GPU state per
// source, so pass this to Renderer.ReleaseSource when the scene is done with — scene
// teardown does not reach into a renderer to do it, and a renderer that never hears
// about the destruction would hold the cache forever.
func (s *Scene) ID() SourceID {
	return s.sourceID
}

// Root returns the scene's root node.
func (s *Scene) Root() Node {
	return Node{scene: s, id: s.root}
}

// Add parents a node under the scene root.
func (s *Scene) Add(n SceneNode) {
	s.reparent(n.ID(), s.root)
}

// NewGroup creates an empty group node (hierarchy only).
func (s *Scene) NewGroup() Group {
	return Group{Node{scene: s, id: s.allocNode(KindGroup)}}
}

// SetAmbient sets the ambient light term.
func (s *Scene) SetAmbient(color colors.RGB32F) {
	s.ambient = color
}

// SetFog sets the scene's distance fog, or clears it when f is nil (the default).
// Pass one of the fog models — scene.SetFog(pix.NewExp2Fog(color, 60000)) — and keep
// the returned value if you want to animate its fields; they are re-read every frame.
func (s *Scene) SetFog(fog Fog) {
	s.fog = fog
}

// Fog returns the scene's distance fog, or nil when there is none.
func (s *Scene) Fog() Fog {
	return s.fog
}

// AddDirectionalLight adds a directional light (dir = travel direction) and returns
// its handle — configure it further or call CastShadow on the returned light.
func (s *Scene) AddDirectionalLight(dir glm.Vec3f, color colors.RGB32F, intensity float32) *DirectionalLight {
	l := &DirectionalLight{Direction: dir, Color: color, Intensity: intensity, id: s.newLightID()}
	s.dirLights = append(s.dirLights, l)
	return l
}

// AddPointLight adds a point light at pos with linear falloff to zero at rng and
// returns its handle.
func (s *Scene) AddPointLight(pos glm.Vec3f, color colors.RGB32F, intensity, rng float32) *PointLight {
	l := &PointLight{Position: pos, Color: color, Intensity: intensity, Range: rng, id: s.newLightID()}
	s.pointLights = append(s.pointLights, l)
	return l
}

// AddSpotLight adds a cone light at pos aimed along dir, full inside the inner cone and
// falling to zero at half-angle angle (radians) / distance rng. penumbra (0..1) sets
// the soft-edge fraction. Returns its handle.
func (s *Scene) AddSpotLight(pos, dir glm.Vec3f, color colors.RGB32F, intensity, rng, angle, penumbra float32) *SpotLight {
	l := &SpotLight{
		Position: pos, Direction: dir, Color: color, Intensity: intensity,
		Range: rng, Angle: angle, Penumbra: penumbra, id: s.newLightID(),
	}
	s.spotLights = append(s.spotLights, l)
	return l
}

func (s *Scene) allocNode(kind NodeKind) NodeID {
	var idx uint32
	if s.freeHead != invalidIdx {
		idx = s.freeHead
		s.freeHead = s.parents[idx].index
		s.resetSlot(idx, kind)
	} else {
		idx = uint32(len(s.parents))
		s.parents = append(s.parents, NodeID{})
		s.firstChildren = append(s.firstChildren, NodeID{})
		s.lastChildren = append(s.lastChildren, NodeID{})
		s.nextSiblings = append(s.nextSiblings, NodeID{})
		s.prevSiblings = append(s.prevSiblings, NodeID{})
		s.local = append(s.local, glm.Mat4fIdentity)
		s.world = append(s.world, glm.Mat4fIdentity)
		s.transforms = append(s.transforms, defaultTransform)
		s.flags = append(s.flags, flagAlive|flagLocalVisible|flagCastShadow|flagReceiveShadow|flagDirty|flagVisibleDirty)
		s.generation = append(s.generation, 1)
		s.kind = append(s.kind, kind)
		s.payload = append(s.payload, 0)
		s.names = append(s.names, "")
		s.topoPos = append(s.topoPos, invalidIdx)
		// A grown s.world shifts where collectDrawables addresses instanceTransforms
		// (InstancedMesh drawables use len(s.world) as their base offset — see
		// instanced_mesh.go) — every drawable built against the old length would read
		// the wrong row otherwise. Marking dirty here, on every new slot regardless of
		// this node's own kind, guarantees drawables are never rebuilt against a stale
		// length.
		s.drawableDirty = true
	}
	return NodeID{index: idx, gen: s.generation[idx]}
}

func (s *Scene) resetSlot(idx uint32, kind NodeKind) {
	s.parents[idx] = NodeID{}
	s.firstChildren[idx] = NodeID{}
	s.lastChildren[idx] = NodeID{}
	s.nextSiblings[idx] = NodeID{}
	s.prevSiblings[idx] = NodeID{}
	s.local[idx] = glm.Mat4fIdentity
	s.world[idx] = glm.Mat4fIdentity
	s.transforms[idx] = defaultTransform
	s.flags[idx] = flagAlive | flagLocalVisible | flagCastShadow | flagReceiveShadow | flagDirty | flagVisibleDirty
	s.kind[idx] = kind
	s.payload[idx] = 0
	s.names[idx] = ""
	// Not necessarily invalidIdx already: this slot may be a recycled node whose
	// old topoOrder entry (if any) is still a live tombstone target for
	// detachFromParent/reparent's fast paths to find — but that entry belonged to
	// the PREVIOUS occupant, already invalidated when it was destroyed (destroyNode
	// always detaches first). A fresh slot starts detached either way.
	s.topoPos[idx] = invalidIdx
}

func (s *Scene) validate(id NodeID) {
	if !id.isValid() || id.index >= uint32(len(s.generation)) {
		panic("scene: invalid NodeID")
	}
	if s.generation[id.index] != id.gen {
		panic("scene: stale NodeID")
	}
	if s.flags[id.index]&flagAlive == 0 {
		panic("scene: node has been destroyed")
	}
}

func (s *Scene) reparent(child, newParent NodeID) {
	s.validate(child)
	s.validate(newParent)
	if s.wouldCycle(child, newParent) {
		panic("scene: reparent would create a cycle")
	}
	s.detachFromParent(child)
	s.parents[child.index] = newParent
	last := s.lastChildren[newParent.index]
	if !last.isValid() {
		s.firstChildren[newParent.index] = child
	} else {
		s.nextSiblings[last.index] = child
		s.prevSiblings[child.index] = last
	}
	s.lastChildren[newParent.index] = child
	s.flags[child.index] |= flagDirty
	// A leaf attaching under an already-attached parent can be appended straight
	// to the end of topoOrder: the parent (and everything above it) is already
	// somewhere earlier in the array, so parent-before-child holds trivially — no
	// walk needed. detachFromParent just ran above, so if child had a live entry
	// from a previous attachment it is already tombstoned; this never leaves two
	// live entries for the same node. Anything else (child has children, so its
	// whole subtree's flagAttached needs recomputing; or newParent isn't attached,
	// so child isn't actually visible yet either) falls back to the existing full
	// rebuild.
	leaf := !s.firstChildren[child.index].isValid()
	parentAttached := s.flags[newParent.index]&flagAttached != 0
	if leaf && parentAttached && !s.topoDirty {
		s.topoPos[child.index] = uint32(len(s.topoOrder))
		s.topoOrder = append(s.topoOrder, child.index)
		s.flags[child.index] |= flagAttached
	} else {
		s.topoDirty = true
	}
	// Reparenting can change flagAttached for child (and everything under it) once
	// flushTopoIfDirty runs — which shifts every OTHER attached mesh's position in
	// whatever collectDrawables produces next, not just child's own. Unconditional
	// here for the same reason destroyNode's is: cheap to over-trigger, and a
	// narrower "only if this specific node..." check would miss the reindexing
	// risk to unrelated meshes. Separate concern from the topology bookkeeping
	// above, which is why it doesn't follow the fast/slow branch.
	s.drawableDirty = true
}

func (s *Scene) detachFromParent(child NodeID) {
	p := s.parents[child.index]
	if !p.isValid() {
		return
	}
	prev := s.prevSiblings[child.index]
	next := s.nextSiblings[child.index]
	if prev.isValid() {
		s.nextSiblings[prev.index] = next
	} else {
		s.firstChildren[p.index] = next
	}
	if next.isValid() {
		s.prevSiblings[next.index] = prev
	} else {
		s.lastChildren[p.index] = prev
	}
	s.parents[child.index] = NodeID{}
	s.prevSiblings[child.index] = NodeID{}
	s.nextSiblings[child.index] = NodeID{}
	// A leaf (no children) can be pulled out of topoOrder in place, without the
	// full rebuild every other case needs: nothing else in the tree depends on
	// its position, and it has no descendants whose own flagAttached would go
	// stale. Tombstone its entry (a live entry can never equal invalidIdx) rather
	// than compact the array, so this stays O(1) instead of an O(topoOrder) shift;
	// flushTopoIfDirty is what eventually reclaims the dead slot (see its comment).
	// A node WITH children still needs the full rebuild — nothing else recomputes
	// flagAttached for a whole detached subtree — so it falls back to the
	// unconditional topoDirty every other structural change already used.
	if leaf := !s.firstChildren[child.index].isValid(); leaf && !s.topoDirty {
		if pos := s.topoPos[child.index]; pos != invalidIdx {
			s.topoOrder[pos] = invalidIdx
			s.topoPos[child.index] = invalidIdx
			s.topoHoles++
		}
		s.flags[child.index] &^= flagAttached
		// Keep tombstones under half the array: bounds topoOrder's growth for a
		// scene that only ever attaches/detaches leaves (e.g. continuous
		// bullet-hole-style spawning) instead of letting it fill up with dead
		// entries forever. The 64 floor avoids rebuilding a tiny scene on every
		// other churn.
		if s.topoHoles > len(s.topoOrder)/2 && len(s.topoOrder) > 64 {
			s.topoDirty = true
		}
	} else {
		s.topoDirty = true
	}
	// See reparent's comment: detaching (whether standalone via Node.Remove, or as
	// reparent's first step) can drop child out of flagAttached, reindexing every
	// OTHER attached mesh in collectDrawables' next output. Drawable-list content
	// is a separate concern from the topology bookkeeping above — unconditional
	// here regardless of which path that took.
	s.drawableDirty = true
}

func (s *Scene) wouldCycle(child, newParent NodeID) bool {
	cur := newParent
	for cur.isValid() {
		if cur == child {
			return true
		}
		cur = s.parents[cur.index]
	}
	return false
}

func (s *Scene) destroySubtree(id NodeID) {
	if !id.isValid() || s.flags[id.index]&flagAlive == 0 {
		return
	}
	var kids []NodeID
	c := s.firstChildren[id.index]
	for c.isValid() {
		kids = append(kids, c)
		c = s.nextSiblings[c.index]
	}
	for _, k := range kids {
		s.destroySubtree(k)
	}
	s.destroyNode(id)
}

func (s *Scene) destroyNode(id NodeID) {
	idx := id.index
	s.detachFromParent(id)
	switch s.kind[idx] {
	case KindMesh:
		s.swapRemoveMesh(s.payload[idx])
	case KindSkinnedMesh:
		s.freeSkinnedMesh(s.payload[idx])
	case KindSkeleton:
		s.freeSkeleton(s.payload[idx])
	case KindParticleContainer:
		s.swapRemoveParticles(s.payload[idx])
	case KindInstancedMesh:
		s.swapRemoveInstancedMesh(s.payload[idx])
	}
	s.flags[idx] &^= flagAlive
	s.generation[idx]++
	s.parents[idx] = NodeID{index: s.freeHead}
	s.freeHead = idx
	//TODO: in the future detect if drawable needs to be rebuilt
	s.drawableDirty = true
	// No topoDirty here: detachFromParent above already set it correctly (fast or
	// slow path). destroyNode is only ever reached from destroySubtree, bottom-up
	// after every descendant is already gone (grep confirms the sole call site),
	// so idx is always a leaf here — every single-node destroy is fast-path
	// eligible, and a whole subtree destroy becomes N fast-path detaches instead
	// of N full topology rebuilds.
}

func (s *Scene) swapRemoveMesh(payloadIdx uint32) {
	md := &s.meshes[payloadIdx]
	for _, l := range md.lods {
		l.geometry.Release()
		l.material.Release()
	}
	last := uint32(len(s.meshes) - 1)
	if payloadIdx != last {
		s.meshes[payloadIdx] = s.meshes[last]
		s.payload[s.meshes[payloadIdx].ownerNode] = payloadIdx
	}
	s.meshes = s.meshes[:last]
}

func (s *Scene) flushTopoIfDirty() {
	if !s.topoDirty {
		return
	}
	s.topoOrder = s.topoOrder[:0]
	for i := range s.flags {
		s.flags[i] &^= flagAttached
		// Full rebuild is the authoritative reset for topoPos too: whatever a
		// fast-path append left behind (detachFromParent/reparent) gets wiped here
		// regardless, so those paths never need to stay consistent with a rebuild
		// that supersedes them.
		s.topoPos[i] = invalidIdx
	}
	s.topoHoles = 0
	queue := []uint32{s.root.index}
	for len(queue) > 0 {
		idx := queue[0]
		queue = queue[1:]
		if s.flags[idx]&flagAlive == 0 {
			continue
		}
		s.topoPos[idx] = uint32(len(s.topoOrder))
		s.topoOrder = append(s.topoOrder, idx)
		s.flags[idx] |= flagAttached
		child := s.firstChildren[idx]
		for child.isValid() {
			queue = append(queue, child.index)
			child = s.nextSiblings[child.index]
		}
	}
	s.topoDirty = false
}

// updateTransforms recomputes local + world matrices for dirty nodes in topological
// (parent-before-child) order. Returns true if anything changed. Called only from
// Sync, which flushes topology once up front — this assumes topoOrder/flagAttached
// are already current and does not flush them itself.
func (s *Scene) updateTransforms() bool {
	anyDirty := false
	for _, i := range s.topoOrder {
		// A tombstone left by detachFromParent's fast path (see its doc comment) —
		// not a real node index.
		if i == invalidIdx {
			continue
		}
		if s.flags[i]&flagDirty == 0 {
			continue
		}
		anyDirty = true
		s.local[i] = s.transforms[i].Matrix()
		p := s.parents[i]
		if !p.isValid() {
			s.world[i] = s.local[i]
		} else {
			s.world[i] = s.world[p.index].Mul4x4(s.local[i])
		}
		s.flags[i] &^= flagDirty
		child := s.firstChildren[i]
		for child.isValid() {
			s.flags[child.index] |= flagDirty
			child = s.nextSiblings[child.index]
		}
	}
	return anyDirty
}

// Sync writes the scene's own per-scene GPU state: the world matrices (recomputed
// from dirty transforms), skinning (joint matrices + bounds), and the light table.
// All of it lands in MemoryHost buffers the scene owns directly — no uploader. The
// renderer drives geometry/pipeline syncing (which do need staging, into shared
// MemoryDevice buffers) and the draw-list rebuild separately.
func (s *Scene) Sync() {
	// updateTransforms walks topoOrder, so attachment must be current first.
	s.flushTopoIfDirty()
	if s.updateTransforms() {
		s.transformsDirty = true
	}
	s.syncSkinning()
	// Light objects are mutable, so re-derive the flat GPU table each frame; rebuild
	// only marks the buffer dirty (→ re-writes) when the derived table changed. Both
	// this and drawList.sync above write straight to MemoryHost buffers — nothing
	// scene-owned goes through the shared uploader, so a frame where nothing but
	// (say) an animated character's pose changed stages/submits nothing extra.
	// elapsed feeds every draw/particle root's time field (drawable.go, particle.go) —
	// computed once here rather than by each call site, and passed explicitly rather
	// than read back off the scene by the renderer, matching how every other
	// per-frame value (viewProj, eye) already reaches fillDrawRoots.
	s.elapsed = float32(time.Since(s.clockStart).Seconds())
}

// MeshCount returns the number of mesh nodes in the scene.
func (s *Scene) MeshCount() int {
	return len(s.meshes)
}

// FindByName returns the first live node with the given name (see Node.Name),
// or false if none has it. A linear scan — fine for occasional lookups (finding
// a named bone or prop after loading an asset), not meant for a per-frame path.
// Name is not required to be unique; ties resolve to whichever node happens to
// come first in node-slot order.
func (s *Scene) FindByName(name string) (Node, bool) {
	for i, n := range s.names {
		if n == name && s.flags[i]&flagAlive != 0 {
			return Node{scene: s, id: NodeID{index: uint32(i), gen: s.generation[i]}}, true
		}
	}
	return Node{}, false
}

// FrameSphere returns a robust center + radius for the mesh nodes' world-space
// bounds (median center, percentile-of-center-distances). Run Sync first.
func (s *Scene) FrameSphere(pct float32) (center glm.Vec3f, radius float32) {
	n := len(s.meshes) + s.skinnedMeshes.Len()
	if n == 0 {
		return glm.Vec3f{}, 1
	}
	worldCenter := func(m glm.Mat4f, c glm.Vec3f) glm.Vec3f {
		return glm.Vec3f{
			m[0]*c[0] + m[4]*c[1] + m[8]*c[2] + m[12],
			m[1]*c[0] + m[5]*c[1] + m[9]*c[2] + m[13],
			m[2]*c[0] + m[6]*c[1] + m[10]*c[2] + m[14],
		}
	}
	// worldRadius scales a local bounding radius by the node's dominant axis
	// scale — an approximation (exact for uniform scale), matching the same
	// estimate scene_cull.comp uses. Folded into each entry's own "reach" below
	// so a scene with few (or one) object doesn't collapse to a near-zero radius
	// just because their centers coincide (e.g. a single skinned character's
	// several sub-meshes).
	worldRadius := func(m glm.Mat4f, r float32) float32 {
		return r * maxColumnLength(m)
	}
	// Scratch is retained on the Scene: this runs every frame (prepareShadows fits
	// the directional shadow camera from it), so it must not allocate per call.
	centers := s.frameCenters[:0]
	reach := s.frameReach[:0]
	for i := range s.meshes {
		md := &s.meshes[i]
		m := s.world[md.ownerNode]
		centers = append(centers, worldCenter(m, md.bounds.Center))
		reach = append(reach, worldRadius(m, md.bounds.Radius))
	}
	for _, sm := range s.skinnedMeshes.All() {
		m := s.world[s.skeletons.Get(sm.skeleton).ownerNode]
		centers = append(centers, worldCenter(m, sm.bounds.Center))
		reach = append(reach, worldRadius(m, sm.bounds.Radius))
	}
	s.frameCenters, s.frameReach = centers, reach

	n = len(centers)
	if cap(s.frameScratch) < n {
		s.frameScratch = make([]float32, n)
	}
	scratch := s.frameScratch[:n]

	// Only one order statistic is ever needed per axis, so select it directly
	// rather than sorting the whole axis (which is what made this O(n²)).
	median := func(axis int) float32 {
		for i, c := range centers {
			scratch[i] = c[axis]
		}
		return selectNth(scratch, n/2)
	}
	center = glm.Vec3f{median(0), median(1), median(2)}

	for i, c := range centers {
		scratch[i] = c.Sub(center).Length() + reach[i]
	}
	radius = selectNth(scratch, int(float32(n-1)*clamp01(pct)))
	if radius <= 0 {
		radius = 1
	}
	return center, radius
}

func clamp01(x float32) float32 {
	if x < 0 {
		return 0
	}
	if x > 1 {
		return 1
	}
	return x
}

// selectNth reorders v so that v[k] holds the value it would have if v were fully
// sorted, and returns it — Hoare quickselect, O(n) average. FrameSphere only ever
// wants one order statistic (a median per axis, then a percentile of distances),
// so sorting the whole slice for it is pure waste.
func selectNth(v []float32, k int) float32 {
	lo, hi := 0, len(v)-1
	for lo < hi {
		p := partitionFloat32(v, lo, hi)
		if k <= p {
			hi = p
		} else {
			lo = p + 1
		}
	}
	return v[k]
}

// partitionFloat32 Hoare-partitions v[lo:hi+1] about a median-of-three pivot and
// returns an index p with everything in v[lo..p] <= everything in v[p+1..hi].
//
// Median-of-three earns its keep here rather than being cargo-culted: scene
// objects are very often laid out on a grid or along an axis, so the input
// arrives nearly sorted — exactly the case a first- or last-element pivot
// degenerates to O(n²) on, which is the bug this replaced.
func partitionFloat32(v []float32, lo, hi int) int {
	a, b, c := v[lo], v[lo+(hi-lo)/2], v[hi]
	pivot := max(min(a, b), min(max(a, b), c))
	i, j := lo-1, hi+1
	for {
		for {
			i++
			if v[i] >= pivot {
				break
			}
		}
		for {
			j--
			if v[j] <= pivot {
				break
			}
		}
		if i >= j {
			return j
		}
		v[i], v[j] = v[j], v[i]
	}
}

// Destroy releases the scene's GPU buffers, lights and mesh resource references.
func (s *Scene) Destroy() {
	for i := range s.meshes {
		for _, l := range s.meshes[i].lods {
			l.geometry.Release()
			l.material.Release()
		}
	}
	s.meshes = nil
	for i := range s.instancedMeshes {
		for _, l := range s.instancedMeshes[i].lods {
			l.geometry.Release()
			l.material.Release()
		}
	}
	s.instancedMeshes = nil
	s.instanceTransforms = nil
	for _, sm := range s.skinnedMeshes.All() {
		sm.srcGeometry.Release()
		sm.outputGeo.Release()
		sm.material.Release()
	}
}
