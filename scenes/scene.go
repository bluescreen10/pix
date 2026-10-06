// Package scenes provides a scene graph: a hierarchy of nodes with transforms, meshes,
// instanced and skinned meshes, lights, particle systems and decals.
//
// A Scene is therefore one possible producer of frames rather than a required one; the
// Producer interface is the whole contract, and an ECS can satisfy it without using
// any of this package's types.
package scenes

import (
	"time"

	"github.com/bluescreen10/pix/colors"
	"github.com/bluescreen10/pix/glm"
	"github.com/bluescreen10/pix/mem"
)

const invalidIndex = ^uint32(0)

// nodeKind tags a node's payload table.
type nodeKind uint8

const (
	kindGroup nodeKind = iota
	kindMesh
	kindInstancedMesh
	kindBone
	kindSkeleton
	kindSkinnedMesh
	kindParticleContainer
	kindCamera
)

// nodeFlags is the per-node flag bitset.
type nodeFlags uint32

const (
	flagAlive nodeFlags = 1 << iota
	flagCastShadow
	flagReceiveShadow
	flagTransformDirty
	flagLocalVisible
	flagVisibleDirty
	flagVisible
	// flagAttached marks a node reachable from the scene root. Maintained by
	// updateTopology, which already computes exactly that set. Only attached
	// nodes get their world matrix updated, so only attached meshes may draw —
	// see collectDrawables.
	flagAttached
)

// NodeID is a generation-counted handle. Its zero value is invalid because generations
// start at 1.
type NodeID struct {
	index uint32
	gen   uint32
}

func (id NodeID) isValid() bool {
	return id.gen != 0
}

// Scene manages a node hierarchy, transforms, drawable payloads, and lights. Extract
// publishes the current rendering description.
type Scene struct {
	// root is the permanent root group created with the scene.
	root NodeID

	// parents records each live node's parent. Freed slots use the index as the
	// next link in the free list.
	parents []NodeID

	// firstChildren records the first child in each node's sibling list.
	firstChildren []NodeID

	// lastChildren records the last child in each node's sibling list.
	lastChildren []NodeID

	// nextSiblings links each node to its next sibling.
	nextSiblings []NodeID

	// prevSiblings links each node to its previous sibling.
	prevSiblings []NodeID

	// freeHead is the first reusable node slot, or invalidIndex when none are free.
	freeHead uint32

	// world caches each node's world-space transform matrix.
	world []glm.Mat4f

	// transforms stores the editable transform components for each node.
	transforms []Transform

	// flags stores lifecycle, visibility, shadow, and transform state for each node.
	flags []nodeFlags

	// generation stores the current generation for each node slot. A NodeID captures
	// this value when created so validation can reject stale handles after slot reuse.
	generation []uint32

	// kind identifies the payload table used by each node.
	kind []nodeKind

	// payload stores each node's index in its kind-specific payload table.
	payload []uint32

	// names stores optional user-facing node names; an empty string means unnamed.
	names []string

	// topologyOrder lists attached node slots in parent-before-child order. Detached
	// leaves may leave invalidIndex tombstones until the next full rebuild.
	topologyOrder []uint32

	// topologyPositions maps each node slot to its position in topologyOrder, or to
	// invalidIndex when it has no live entry.
	topologyPositions []uint32

	// topologyHoles counts invalidIndex tombstones in topologyOrder.
	topologyHoles int

	// topologyDirty requests a full topology rebuild before the next traversal.
	topologyDirty bool

	// meshes stores the payload for every regular mesh node.
	meshes []meshData

	// instancedMeshes stores the payload for every instanced mesh node.
	instancedMeshes []instancedMeshData

	// instanceTransforms stores per-instance matrices outside the node hierarchy. Frame
	// packets address them immediately after the node world matrices.
	instanceTransforms []glm.Mat4f

	// particleContainers stores the payload for every particle container node.
	particleContainers []particleData

	// nextParticleID is the last stable particle-system identity issued by the scene.
	nextParticleID ParticleID

	// skeletons stores stable slab entries for bone hierarchies and inverse bind data.
	skeletons mem.Slab[skeletonData]

	// skinnedMeshes stores stable slab entries because each entry refers to a skeleton
	// by slab ID.
	skinnedMeshes mem.Slab[skinnedMeshData]

	// fog is the active distance-fog model, or nil when fog is disabled.
	fog Fog
	// environment is the light surrounding the scene, or nil for flat ambient light.
	environment *Environment

	// ambient is the colour of the scene-wide ambient light, and ambientIntensity how
	// bright it is (see SetAmbient).
	ambient          colors.RGB32F
	ambientIntensity float32

	// dirLights contains the scene's directional lights.
	dirLights []*DirectionalLight

	// pointLights contains the scene's point lights.
	pointLights []*PointLight

	// spotLights contains the scene's spot lights.
	spotLights []*SpotLight

	// nextLightID is the last stable light identity issued by the scene.
	nextLightID LightID

	// cameras stores the payload for every camera node, in creation order: that order
	// is the order of the frame's views.
	cameras []cameraData

	// nextViewID is the last stable view identity issued by the scene.
	nextViewID ViewID

	// packet caches the frame description and owns the reusable backing slices that
	// Extract lends to callers. Scene graph storage such as world remains separate and
	// is referenced by the packet rather than copied into it.
	packet FramePacket

	// packetDirty requests a rebuild of the cached mesh, LOD, and material tables.
	packetDirty bool

	// frameCenters retains world-space center scratch used by FrameSphere.
	frameCenters []glm.Vec3f

	// frameReach retains per-object radius scratch used by FrameSphere.
	frameReach []float32

	// frameScratch retains scalar selection scratch used by FrameSphere.
	frameScratch []float32

	// clockStart anchors the scene-wide elapsed time reported by packet.Time.
	clockStart time.Time
}

// New creates an empty scene with a root group. The scene owns no backend or GPU state;
// Extract publishes plain frame data for a renderer to consume.
func New() *Scene {
	s := &Scene{
		packet:        FramePacket{Source: NewSourceID()},
		freeHead:      invalidIndex,
		topologyDirty: true,
		clockStart:    time.Now(),
	}
	s.skeletons = mem.NewSlab[skeletonData]()
	s.skinnedMeshes = mem.NewSlab[skinnedMeshData]()
	s.root = s.allocNode(kindGroup)
	s.flags[s.root.index] = flagAlive | flagLocalVisible | flagVisible
	return s
}

// ID returns the scene's stable identity as a packet producer. Consumers can use it to
// cache per-scene state; IDs are never reused.
func (s *Scene) ID() SourceID {
	return s.packet.Source
}

// Root returns the scene's root node.
func (s *Scene) Root() Node {
	return Node{scene: s, id: s.root}
}

// Add parents a node under the scene root.
func (s *Scene) Add(node SceneNode) {
	s.reparent(node.ID(), s.root)
}

// NewGroup creates an empty group node (hierarchy only).
func (s *Scene) NewGroup() Group {
	return Group{Node: Node{scene: s, id: s.allocNode(kindGroup)}}
}

// SetAmbient sets the light that reaches every surface from every direction, as lights
// are set: its colour, times intensity. The colour is linear light, so it can carry the
// brightness too; intensity is there to turn it up and down without retinting it.
func (s *Scene) SetAmbient(color colors.RGB32F, intensity float32) {
	s.ambient = color
	s.ambientIntensity = intensity
}

// Ambient returns the ambient light's colour (see SetAmbient).
func (s *Scene) Ambient() (colors.RGB32F, float32) {
	return s.ambient, s.ambientIntensity
}

// SetFog sets the scene's distance fog, or clears it when fog is nil (the default).
// Pass one of the fog models — scene.SetFog(pix.NewExp2Fog(color, 60000)) — and keep
// the returned value if you want to animate its fields; they are re-read every frame.
func (s *Scene) SetFog(fog Fog) {
	s.fog = fog
}

// Fog returns the scene's distance fog, or nil when there is none.
func (s *Scene) Fog() Fog {
	return s.fog
}

// AddDirectionalLight adds a directional light and returns its handle. Direction is
// the direction in which the light travels. Configure the returned light further or
// call SetCastShadow to enable shadows.
func (s *Scene) AddDirectionalLight(direction glm.Vec3f, color colors.RGB32F, intensity float32) *DirectionalLight {
	light := &DirectionalLight{
		Direction: direction,
		Color:     color,
		Intensity: intensity,
		id:        s.newLightID(),
	}
	s.dirLights = append(s.dirLights, light)
	return light
}

// AddPointLight adds a point light at position with linear falloff to zero at
// maxDistance and returns its handle.
func (s *Scene) AddPointLight(position glm.Vec3f, color colors.RGB32F, intensity, maxDistance float32) *PointLight {
	light := &PointLight{
		Position:  position,
		Color:     color,
		Intensity: intensity,
		Range:     maxDistance,
		id:        s.newLightID(),
	}
	s.pointLights = append(s.pointLights, light)
	return light
}

// AddSpotLight adds a cone light at position aimed along direction. It has full
// intensity inside the inner cone and falls to zero at the outer half-angle and at
// maxDistance. The outer angle is measured in radians; penumbra (0..1) sets the
// soft-edge fraction.
func (s *Scene) AddSpotLight(position, direction glm.Vec3f, color colors.RGB32F, intensity, maxDistance, outerAngle, penumbra float32) *SpotLight {
	light := &SpotLight{
		Position:  position,
		Direction: direction,
		Color:     color,
		Intensity: intensity,
		Range:     maxDistance,
		Angle:     outerAngle,
		Penumbra:  penumbra,
		id:        s.newLightID(),
	}
	s.spotLights = append(s.spotLights, light)
	return light
}

func (s *Scene) allocNode(kind nodeKind) NodeID {
	var idx uint32
	if s.freeHead != invalidIndex {
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
		s.world = append(s.world, glm.Mat4fIdentity)
		s.transforms = append(s.transforms, defaultTransform)
		s.flags = append(s.flags, flagAlive|flagLocalVisible|flagVisible|flagCastShadow|flagReceiveShadow|flagTransformDirty|flagVisibleDirty)
		s.generation = append(s.generation, 1)
		s.kind = append(s.kind, kind)
		s.payload = append(s.payload, 0)
		s.names = append(s.names, "")
		s.topologyPositions = append(s.topologyPositions, invalidIndex)
		// A grown s.world shifts where collectDrawables addresses instanceTransforms
		// (InstancedMesh drawables use len(s.world) as their base offset — see
		// instanced_mesh.go) — every drawable built against the old length would read
		// the wrong row otherwise. Marking dirty here, on every new slot regardless of
		// this node's own kind, guarantees drawables are never rebuilt against a stale
		// length.
		s.packetDirty = true
	}
	return NodeID{index: idx, gen: s.generation[idx]}
}

func (s *Scene) resetSlot(idx uint32, kind nodeKind) {
	s.parents[idx] = NodeID{}
	s.firstChildren[idx] = NodeID{}
	s.lastChildren[idx] = NodeID{}
	s.nextSiblings[idx] = NodeID{}
	s.prevSiblings[idx] = NodeID{}
	s.world[idx] = glm.Mat4fIdentity
	s.transforms[idx] = defaultTransform
	s.flags[idx] = flagAlive | flagLocalVisible | flagVisible | flagCastShadow | flagReceiveShadow | flagTransformDirty | flagVisibleDirty
	s.kind[idx] = kind
	s.payload[idx] = 0
	s.names[idx] = ""
	// Not necessarily invalidIndex already: this slot may be a recycled node whose
	// old topologyOrder entry (if any) is still a live tombstone target for
	// detachFromParent/reparent's fast paths to find — but that entry belonged to
	// the previous occupant, already invalidated when it was destroyed (destroyNode
	// always detaches first). A fresh slot starts detached either way.
	s.topologyPositions[idx] = invalidIndex
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
	s.flags[child.index] |= flagTransformDirty
	// A leaf attaching under an already-attached parent can be appended straight
	// to the end of topologyOrder: the parent (and everything above it) is already
	// somewhere earlier in the array, so parent-before-child holds trivially — no
	// walk needed. detachFromParent just ran above, so if child had a live entry
	// from a previous attachment it is already tombstoned; this never leaves two
	// live entries for the same node. Anything else (child has children, so its
	// whole subtree's flagAttached needs recomputing; or newParent isn't attached,
	// so child isn't actually visible yet either) falls back to the existing full
	// rebuild.
	leaf := !s.firstChildren[child.index].isValid()
	parentAttached := s.flags[newParent.index]&flagAttached != 0
	if leaf && parentAttached && !s.topologyDirty {
		s.topologyPositions[child.index] = uint32(len(s.topologyOrder))
		s.topologyOrder = append(s.topologyOrder, child.index)
		s.flags[child.index] |= flagAttached
	} else {
		s.topologyDirty = true
	}
	// Reparenting can change flagAttached for child (and everything under it) once
	// updateTopology runs — which shifts every other attached mesh's position in
	// whatever collectDrawables produces next, not just child's own. Unconditional
	// here for the same reason destroyNode's is: cheap to over-trigger, and a
	// narrower "only if this specific node..." check would miss the reindexing
	// risk to unrelated meshes. Separate concern from the topology bookkeeping
	// above, which is why it doesn't follow the fast/slow branch.
	s.packetDirty = true
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
	// A leaf (no children) can be pulled out of topologyOrder in place, without the
	// full rebuild every other case needs: nothing else in the tree depends on
	// its position, and it has no descendants whose own flagAttached would go
	// stale. Tombstone its entry (a live entry can never equal invalidIndex) rather
	// than compact the array, so this stays O(1) instead of an O(topologyOrder) shift;
	// updateTopology is what eventually reclaims the dead slot (see its comment).
	// A node with children still needs the full rebuild — nothing else recomputes
	// flagAttached for a whole detached subtree — so it falls back to the
	// unconditional topologyDirty every other structural change already used.
	if leaf := !s.firstChildren[child.index].isValid(); leaf && !s.topologyDirty {
		if position := s.topologyPositions[child.index]; position != invalidIndex {
			s.topologyOrder[position] = invalidIndex
			s.topologyPositions[child.index] = invalidIndex
			s.topologyHoles++
		}
		s.flags[child.index] &^= flagAttached
		// Keep tombstones under half the array: bounds topologyOrder's growth for a
		// scene that only ever attaches/detaches leaves (e.g. continuous
		// bullet-hole-style spawning) instead of letting it fill up with dead
		// entries forever. The 64 floor avoids rebuilding a tiny scene on every
		// other churn.
		if s.topologyHoles > len(s.topologyOrder)/2 && len(s.topologyOrder) > 64 {
			s.topologyDirty = true
		}
	} else {
		s.topologyDirty = true
	}
	// See reparent's comment: detaching (whether standalone via Node.Remove, or as
	// reparent's first step) can drop child out of flagAttached, reindexing every
	// other attached mesh in collectDrawables' next output. Drawable-list content
	// is a separate concern from the topology bookkeeping above — unconditional
	// here regardless of which path that took.
	s.packetDirty = true
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
	case kindMesh:
		s.swapRemoveMesh(s.payload[idx])
	case kindSkinnedMesh:
		s.freeSkinnedMesh(s.payload[idx])
	case kindSkeleton:
		s.freeSkeleton(s.payload[idx])
	case kindParticleContainer:
		s.swapRemoveParticles(s.payload[idx])
	case kindInstancedMesh:
		s.swapRemoveInstancedMesh(s.payload[idx])
	case kindCamera:
		s.removeCamera(s.payload[idx])
	}
	s.flags[idx] &^= flagAlive
	s.generation[idx]++
	s.parents[idx] = NodeID{index: s.freeHead}
	s.freeHead = idx
	// TODO: Detect whether the packet needs to be rebuilt.
	s.packetDirty = true
	// No topologyDirty here: detachFromParent above already set it correctly (fast or
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

func (s *Scene) updateTopology() {
	if !s.topologyDirty {
		return
	}
	s.topologyOrder = s.topologyOrder[:0]
	for i := range s.flags {
		s.flags[i] &^= flagAttached
		// Full rebuild is the authoritative reset for topologyPositions too: whatever a
		// fast-path append left behind (detachFromParent/reparent) gets wiped here
		// regardless, so those paths never need to stay consistent with a rebuild
		// that supersedes them.
		s.topologyPositions[i] = invalidIndex
	}
	s.topologyHoles = 0
	queue := []uint32{s.root.index}
	for len(queue) > 0 {
		idx := queue[0]
		queue = queue[1:]
		if s.flags[idx]&flagAlive == 0 {
			continue
		}
		s.topologyPositions[idx] = uint32(len(s.topologyOrder))
		s.topologyOrder = append(s.topologyOrder, idx)
		s.flags[idx] |= flagAttached
		child := s.firstChildren[idx]
		for child.isValid() {
			queue = append(queue, child.index)
			child = s.nextSiblings[child.index]
		}
	}
	s.topologyDirty = false
}

// updateTransforms recomputes world matrices for dirty nodes in topological
// (parent-before-child) order. Returns true if anything changed. Called only from
// Sync, which flushes topology once up front — this assumes topologyOrder/flagAttached
// are already current and does not flush them itself.
func (s *Scene) updateTransforms() {
	anyDirty := false
	for _, i := range s.topologyOrder {
		// A tombstone left by detachFromParent's fast path (see its doc comment) —
		// not a real node index.
		if i == invalidIndex {
			continue
		}

		if s.flags[i]&flagTransformDirty == 0 {
			continue
		}

		anyDirty = true

		// A node's local matrix is needed only here, on the way to its world matrix, so
		// it is not kept. If a caller ever needs it cached, the cache belongs inside
		// Transform, beside the components it is derived from.
		local := s.transforms[i].Matrix()
		parent := s.parents[i]
		if !parent.isValid() {
			s.world[i] = local
		} else {
			s.world[i] = s.world[parent.index].Mul4x4(local)
		}

		// mark self as non-dirty
		s.flags[i] &^= flagTransformDirty

		// mark children as dirty
		child := s.firstChildren[i]
		for child.isValid() {
			s.flags[child.index] |= flagTransformDirty
			child = s.nextSiblings[child.index]
		}
	}
	if anyDirty {
		s.packet.TransformsDirty = true
	}
}

// Sync updates derived per-frame scene state, including world transforms, skinning
// data, and elapsed time. Extract calls Sync automatically; call it directly before
// reading cached world transforms or bounds.
func (s *Scene) Sync() {
	s.updateTopology()
	s.updateTransforms()
	s.updateSkinning()
	s.packet.Time = float32(time.Since(s.clockStart).Seconds())
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

// FrameSphere returns a robust center and radius for the mesh nodes' world-space
// bounds (median center, percentile-of-center-distances). Values outside the
// percentile range [0, 1] are clamped. Run Sync first.
func (s *Scene) FrameSphere(percentile float32) (center glm.Vec3f, radius float32) {
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
	// Scratch is retained on the Scene: this runs every frame (Renderer.fitShadows fits
	// the directional shadow camera from it), so it must not allocate per call.
	centers := s.frameCenters[:0]
	reach := s.frameReach[:0]
	for i := range s.meshes {
		md := &s.meshes[i]
		m := s.world[md.ownerNode]
		centers = append(centers, worldCenter(m, md.bounds.Center))
		reach = append(reach, worldRadius(m, md.bounds.Radius))
	}
	for _, sm := range s.skinnedMeshes.Entries() {
		m := s.world[s.skeletons.Value(sm.skeleton).ownerNode]
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
	radius = selectNth(scratch, int(float32(n-1)*clamp01(percentile)))
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

// Destroy releases the geometry and material references retained by the scene.
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
	for _, sm := range s.skinnedMeshes.Entries() {
		sm.srcGeometry.Release()
		sm.outputGeo.Release()
		sm.material.Release()
	}
	for _, l := range s.dirLights {
		l.SetMask(LightMask{})
	}
}
