package pix

import "github.com/bluescreen10/pix/glm"

const invalidIdx = ^uint32(0)

// NodeKind identifies what type of data lives in the payload table.
type NodeKind uint8

const (
	KindGroup NodeKind = iota
	KindMesh
	KindDirectionalLight
	KindAmbientLight
)

// Node flags packed into flags[].
type NodeFlags uint32

func (f NodeFlags) IsVisible() bool {
	return f&flagVisible != 0
}

func (f NodeFlags) CastShadow() bool {
	return f&flagCastShadow != 0
}

func (f NodeFlags) IsAlive() bool {
	return f&flagAlive != 0
}

const (
	flagAlive = NodeFlags(1 << iota)
	flagCastShadow
	flagReceiveShadow
	flagDirty
	flagStatic
	flagLocalVisible
	flagVisibleDirty
	flagVisible
)

// NodeID is a generation-counted handle. Zero value is invalid (gen starts at 1).
type NodeID struct {
	index uint32
	gen   uint32
}

func (id NodeID) isValid() bool {
	return id.gen != 0
}

// Scene owns all node state. GPU resources are owned by the Renderer and
// referenced here by pointer (transitional; will become IDs when Renderer
// fully splits out resource ownership per the spec).
type Scene struct {
	// Hierarchy — parallel arrays indexed by slot.
	parents       []NodeID
	firstChildren []NodeID
	lastChildren  []NodeID
	nextSiblings  []NodeID
	prevSiblings  []NodeID

	// Transforms — also parallel arrays.
	local    []glm.Mat4f
	world    []glm.Mat4f
	worldInv []glm.Mat4f

	positions []glm.Vec3f
	rotations []glm.Quatf
	scales    []glm.Vec3f

	// Per-node state.
	flags      []NodeFlags
	generation []uint32
	kind       []NodeKind
	payload    []uint32

	// Allocator: free list threaded through parents[] of dead slots.
	freeHead uint32

	// Topological order (parent-before-child); rebuilt when topoDirty is set.
	topoOrder []uint32
	topoDirty bool

	root NodeID

	background glm.Color4f

	// Kind-specific compact payload tables.
	meshes        []meshData
	dirLights     []directionalLightData
	ambientLights []ambientLightData
}

func NewScene() *Scene {
	s := &Scene{freeHead: invalidIdx, topoDirty: true}
	s.root = s.allocNode(KindGroup)
	// Root is permanently alive, visible, and effectively visible.
	s.flags[s.root.index] = flagAlive | flagLocalVisible | flagVisible
	return s
}

func (s *Scene) Background() glm.Color4f {
	return s.background
}

func (s *Scene) SetBackground(c glm.Color4f) {
	s.background = c
}

// Add parents the node under the scene root.
// Accepts any typed handle (Mesh, Group, DirectionalLight, …).
func (s *Scene) Add(n SceneNode) {
	s.reparent(n.ID(), s.root)
}

func (s *Scene) NewGroup() Group {
	id := s.allocNode(KindGroup)
	return Group{Node{scene: s, id: id}}
}

func (s *Scene) GetFlags(id uint32) NodeFlags {
	return s.flags[id]
}

func (s *Scene) GetWorldTransform(id uint32) glm.Mat4f {
	return s.world[id]
}

func (s *Scene) GetWorldTransformInv(id uint32) glm.Mat4f {
	return s.worldInv[id]
}

// allocNode claims a slot (reusing a freed one when available) and returns its NodeID.
func (s *Scene) allocNode(kind NodeKind) NodeID {
	var idx uint32
	if s.freeHead != invalidIdx {
		idx = s.freeHead
		s.freeHead = s.parents[idx].index // advance free list
		s.resetSlot(idx, kind)
	} else {
		idx = uint32(len(s.parents))
		s.parents = append(s.parents, NodeID{})
		s.firstChildren = append(s.firstChildren, NodeID{})
		s.lastChildren = append(s.lastChildren, NodeID{})
		s.nextSiblings = append(s.nextSiblings, NodeID{})
		s.prevSiblings = append(s.prevSiblings, NodeID{})
		s.local = append(s.local, glm.Mat4fIndentity)
		s.world = append(s.world, glm.Mat4fIndentity)
		s.worldInv = append(s.worldInv, glm.Mat4fIndentity)
		s.positions = append(s.positions, glm.Vec3f{})
		s.rotations = append(s.rotations, glm.QuatIdentityf)
		s.scales = append(s.scales, glm.Vec3f{1, 1, 1})
		s.flags = append(s.flags, flagAlive|flagLocalVisible|flagDirty|flagVisibleDirty)
		s.generation = append(s.generation, 1)
		s.kind = append(s.kind, kind)
		s.payload = append(s.payload, 0)
	}
	return NodeID{index: idx, gen: s.generation[idx]}
}

// resetSlot re-initialises a recycled slot. Generation is NOT touched here;
// destroyNode already bumped it so the new node gets the incremented value.
func (s *Scene) resetSlot(idx uint32, kind NodeKind) {
	s.parents[idx] = NodeID{}
	s.firstChildren[idx] = NodeID{}
	s.lastChildren[idx] = NodeID{}
	s.nextSiblings[idx] = NodeID{}
	s.prevSiblings[idx] = NodeID{}
	s.local[idx] = glm.Mat4fIndentity
	s.world[idx] = glm.Mat4fIndentity
	s.worldInv[idx] = glm.Mat4fIndentity
	s.positions[idx] = glm.Vec3f{}
	s.rotations[idx] = glm.QuatIdentityf
	s.scales[idx] = glm.Vec3f{1, 1, 1}
	s.flags[idx] = flagAlive | flagLocalVisible | flagDirty | flagVisibleDirty
	s.kind[idx] = kind
	s.payload[idx] = 0
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

	// O(1) append at tail of newParent's child list.
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
	s.topoDirty = true
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
	s.topoDirty = true
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

// destroySubtree destroys the node and its entire subtree (post-order).
func (s *Scene) destroySubtree(id NodeID) {
	s.validate(id)
	child := s.firstChildren[id.index]
	for child.isValid() {
		next := s.nextSiblings[child.index]
		s.destroySubtree(child)
		child = next
	}
	s.destroyNode(id)
}

func (s *Scene) destroyNode(id NodeID) {
	idx := id.index
	s.detachFromParent(id)

	switch s.kind[idx] {
	case KindMesh:
		s.swapRemoveMesh(s.payload[idx])
	case KindDirectionalLight:
		s.swapRemoveDirLight(s.payload[idx])
	case KindAmbientLight:
		s.swapRemoveAmbientLight(s.payload[idx])
	}

	s.flags[idx] &^= flagAlive
	s.generation[idx]++
	// Thread this slot onto the free list via parents[].
	s.parents[idx] = NodeID{index: s.freeHead}
	s.freeHead = idx
	s.topoDirty = true
}

func (s *Scene) swapRemoveMesh(payloadIdx uint32) {
	last := uint32(len(s.meshes) - 1)
	// Release refs owned by the destroyed mesh payload.
	s.meshes[payloadIdx].geometry.Release()
	s.meshes[payloadIdx].material.Release()
	if payloadIdx < last {
		s.meshes[payloadIdx] = s.meshes[last]
		s.payload[s.meshes[payloadIdx].ownerNode] = payloadIdx
	}
	s.meshes = s.meshes[:last]
}

func (s *Scene) swapRemoveDirLight(payloadIdx uint32) {
	last := uint32(len(s.dirLights) - 1)
	if payloadIdx < last {
		s.dirLights[payloadIdx] = s.dirLights[last]
		s.payload[s.dirLights[payloadIdx].ownerNode] = payloadIdx
	}
	s.dirLights = s.dirLights[:last]
}

func (s *Scene) swapRemoveAmbientLight(payloadIdx uint32) {
	last := uint32(len(s.ambientLights) - 1)
	if payloadIdx < last {
		s.ambientLights[payloadIdx] = s.ambientLights[last]
		s.payload[s.ambientLights[payloadIdx].ownerNode] = payloadIdx
	}
	s.ambientLights = s.ambientLights[:last]
}

// flushTopoIfDirty rebuilds topoOrder with a BFS from root (parent-before-child).
func (s *Scene) flushTopoIfDirty() {
	if !s.topoDirty {
		return
	}
	s.topoOrder = s.topoOrder[:0]

	queue := []uint32{s.root.index}
	for len(queue) > 0 {
		idx := queue[0]
		queue = queue[1:]

		if s.flags[idx]&flagAlive == 0 {
			continue
		}
		s.topoOrder = append(s.topoOrder, idx)

		child := s.firstChildren[idx]
		for child.isValid() {
			queue = append(queue, child.index)
			child = s.nextSiblings[child.index]
		}
	}
	s.topoDirty = false
}

// UpdateTransforms recomputes local and world matrices for all dirty nodes in
// topological order. Must be called before any render pass that reads world[].
func (s *Scene) UpdateTransforms() {
	s.flushTopoIfDirty()

	for _, i := range s.topoOrder {
		if s.flags[i]&flagDirty == 0 {
			continue
		}

		s.local[i] = glm.Transform(s.scales[i], s.rotations[i], s.positions[i])

		p := s.parents[i]
		if !p.isValid() {
			s.world[i] = s.local[i]
		} else {
			s.world[i] = s.world[p.index].Mul4x4(s.local[i])
		}
		s.worldInv[i] = s.world[i].Inv()

		s.flags[i] &^= flagDirty

		// Propagate dirty to direct children. Because topoOrder is parent-before-child,
		// children have not been visited yet and will see the flag in this same pass.
		// This makes the pass self-correct even if a child wasn't pre-marked dirty.
		child := s.firstChildren[i]
		for child.isValid() {
			s.flags[child.index] |= flagDirty
			child = s.nextSiblings[child.index]
		}
	}
}

// UpdateVisibility propagates effectiveVisible flags in topological order.
func (s *Scene) UpdateVisibility() {
	for _, i := range s.topoOrder {
		if s.flags[i]&flagVisibleDirty == 0 {
			continue
		}

		localVisible := s.flags[i]&flagLocalVisible != 0
		p := s.parents[i]

		var parentVisible bool
		if !p.isValid() {
			parentVisible = true
		} else {
			parentVisible = s.flags[p.index]&flagVisible != 0
		}

		if localVisible && parentVisible {
			s.flags[i] |= flagVisible
		} else {
			s.flags[i] &^= flagVisible
		}

		child := s.firstChildren[i]
		for child.isValid() {
			s.flags[child.index] |= flagVisibleDirty
			child = s.nextSiblings[child.index]
		}
	}
}
