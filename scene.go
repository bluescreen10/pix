package pix

import (
	"math"

	"github.com/bluescreen10/pix/glm"
	"github.com/bluescreen10/pix/gpu"
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
	KindSkinnedMesh
	KindAmbientLight
	KindDirectionalLight
	KindSpotLight
	KindPointLight
)

// NodeFlags is the per-node flag bitset.
type NodeFlags uint32

const (
	flagAlive NodeFlags = 1 << iota
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

func (id NodeID) isValid() bool { return id.gen != 0 }

// Scene owns the node scene graph (flat parallel arrays, linked-list hierarchy),
// the per-node transforms, the mesh payloads (which hold ref-counted handles to
// renderer-owned geometry/materials), the scene lights, and its own per-scene GPU
// buffers (world matrices, drawables, cull state). It is decoupled from the concrete
// Renderer — it depends only on gpu.Backend (obtained via Renderer.NewScene).
type Scene struct {
	backend gpu.Backend

	parents       []NodeID
	firstChildren []NodeID
	lastChildren  []NodeID
	nextSiblings  []NodeID
	prevSiblings  []NodeID

	local    []glm.Mat4f
	world    []glm.Mat4f
	worldInv []glm.Mat4f

	transforms []Transform

	flags      []NodeFlags
	generation []uint32
	kind       []NodeKind
	payload    []uint32

	freeHead uint32

	topoOrder []uint32
	topoDirty bool
	root      NodeID

	meshes []meshData

	// Light objects the scene owns; the flat GPU table (lights) is derived from them
	// (+ ambient) each frame in Sync.
	ambient     glm.Vec3f
	dirLights   []*DirectionalLight
	pointLights []*PointLight
	spotLights  []*SpotLight
	lights      *Lights

	drawableDirty bool
	drawList      *drawList
}

// NewScene creates an empty scene bound to a backend (usually via Renderer.NewScene).
func NewScene(backend gpu.Backend) *Scene {
	s := &Scene{backend: backend, freeHead: invalidIdx, topoDirty: true}
	s.lights = NewLights(backend)
	s.drawList = newDrawList(backend)
	s.root = s.allocNode(KindGroup)
	s.flags[s.root.index] = flagAlive | flagLocalVisible | flagVisible
	return s
}

// Root returns the scene's root node.
func (s *Scene) Root() Node { return Node{scene: s, id: s.root} }

// Add parents a node under the scene root.
func (s *Scene) Add(n SceneNode) { s.reparent(n.ID(), s.root) }

// NewGroup creates an empty group node (hierarchy only).
func (s *Scene) NewGroup() Group {
	return Group{Node{scene: s, id: s.allocNode(KindGroup)}}
}

// SetAmbient sets the ambient light term.
func (s *Scene) SetAmbient(c glm.Vec3f) { s.ambient = c }

// AddDirectionalLight adds a directional light (dir = travel direction) and returns
// its handle — configure it further or call CastShadow on the returned light.
func (s *Scene) AddDirectionalLight(dir, color glm.Vec3f, intensity float32) *DirectionalLight {
	l := &DirectionalLight{Direction: dir, Color: color, Intensity: intensity}
	s.dirLights = append(s.dirLights, l)
	return l
}

// AddPointLight adds a point light at pos with linear falloff to zero at rng and
// returns its handle.
func (s *Scene) AddPointLight(pos, color glm.Vec3f, intensity, rng float32) *PointLight {
	l := &PointLight{Position: pos, Color: color, Intensity: intensity, Range: rng}
	s.pointLights = append(s.pointLights, l)
	return l
}

// AddSpotLight adds a cone light at pos aimed along dir, full inside the inner cone and
// falling to zero at half-angle angle (radians) / distance rng. penumbra (0..1) sets
// the soft-edge fraction. Returns its handle.
func (s *Scene) AddSpotLight(pos, dir, color glm.Vec3f, intensity, rng, angle, penumbra float32) *SpotLight {
	l := &SpotLight{
		Position: pos, Direction: dir, Color: color, Intensity: intensity,
		Range: rng, Angle: angle, Penumbra: penumbra,
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
		s.local = append(s.local, glm.Mat4fIndentity)
		s.world = append(s.world, glm.Mat4fIndentity)
		s.worldInv = append(s.worldInv, glm.Mat4fIndentity)
		s.transforms = append(s.transforms, defaultTransform)
		s.flags = append(s.flags, flagAlive|flagLocalVisible|flagCastShadow|flagReceiveShadow|flagDirty|flagVisibleDirty)
		s.generation = append(s.generation, 1)
		s.kind = append(s.kind, kind)
		s.payload = append(s.payload, 0)
	}
	return NodeID{index: idx, gen: s.generation[idx]}
}

func (s *Scene) resetSlot(idx uint32, kind NodeKind) {
	s.parents[idx] = NodeID{}
	s.firstChildren[idx] = NodeID{}
	s.lastChildren[idx] = NodeID{}
	s.nextSiblings[idx] = NodeID{}
	s.prevSiblings[idx] = NodeID{}
	s.local[idx] = glm.Mat4fIndentity
	s.world[idx] = glm.Mat4fIndentity
	s.worldInv[idx] = glm.Mat4fIndentity
	s.transforms[idx] = defaultTransform
	s.flags[idx] = flagAlive | flagLocalVisible | flagCastShadow | flagReceiveShadow | flagDirty | flagVisibleDirty
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
	if s.kind[idx] == KindMesh {
		s.swapRemoveMesh(s.payload[idx])
	}
	s.flags[idx] &^= flagAlive
	s.generation[idx]++
	s.parents[idx] = NodeID{index: s.freeHead}
	s.freeHead = idx
	s.drawableDirty = true
	s.topoDirty = true
}

func (s *Scene) swapRemoveMesh(payloadIdx uint32) {
	md := &s.meshes[payloadIdx]
	md.geometry.Release()
	md.material.Release()
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

// UpdateTransforms recomputes local + world matrices for dirty nodes in topological
// (parent-before-child) order. Returns true if anything changed.
func (s *Scene) UpdateTransforms() bool {
	s.flushTopoIfDirty()
	anyDirty := false
	for _, i := range s.topoOrder {
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
		s.worldInv[i] = s.world[i].Inv()
		s.flags[i] &^= flagDirty
		child := s.firstChildren[i]
		for child.isValid() {
			s.flags[child.index] |= flagDirty
			child = s.nextSiblings[child.index]
		}
	}
	return anyDirty
}

// Sync uploads the scene's own per-scene GPU state through the uploader: the world
// matrices (recomputed from dirty transforms) and the light table. The renderer
// drives geometry/pipeline syncing and the draw-list rebuild separately, then flushes
// the shared uploader once. The scene owns these resources, so it owns their upload.
func (s *Scene) Sync(u *uploader) {
	if dirty := s.UpdateTransforms(); dirty {
		s.drawList.sync(s.world) // host-visible, direct write (not staged)
	}
	// Light objects are mutable, so re-derive the flat GPU table each frame; rebuild
	// only marks the buffer dirty (→ re-uploads) when the derived table changed.
	s.lights.rebuild(s.ambient, s.dirLights, s.pointLights, s.spotLights)
	s.lights.Sync(u)
}

// collectDrawables builds the GPU drawable table from the mesh payloads, plus a
// parallel slice of each drawable's material (for batching + store address).
func (s *Scene) collectDrawables() ([]gpuDrawable, []Material) {
	out := make([]gpuDrawable, 0, len(s.meshes))
	materials := make([]Material, 0, len(s.meshes))
	for i := range s.meshes {
		md := &s.meshes[i]
		var flags uint32
		if s.flags[md.ownerNode]&flagCastShadow != 0 {
			flags |= DrawableCastsShadow
		}
		if s.flags[md.ownerNode]&flagReceiveShadow != 0 {
			flags |= DrawableReceivesShadow
		}
		out = append(out, gpuDrawable{
			bounds:      [4]float32{md.bounds.Center[0], md.bounds.Center[1], md.bounds.Center[2], md.bounds.Radius},
			transformID: md.ownerNode,
			geometryID:  md.geometry.id(),
			materialID:  md.material.materialID(),
			flags:       flags,
		})
		materials = append(materials, md.material)
	}
	return out, materials
}

// MeshCount returns the number of mesh nodes in the scene.
func (s *Scene) MeshCount() int { return len(s.meshes) }

// FrameSphere returns a robust center + radius for the mesh nodes' world-space
// bounds (median center, percentile-of-center-distances). Run UpdateTransforms first.
func (s *Scene) FrameSphere(pct float32) (center glm.Vec3f, radius float32) {
	n := len(s.meshes)
	if n == 0 {
		return glm.Vec3f{}, 1
	}
	centers := make([]glm.Vec3f, n)
	for i := range s.meshes {
		md := &s.meshes[i]
		m := s.world[md.ownerNode]
		c := md.bounds.Center
		centers[i] = glm.Vec3f{
			m[0]*c[0] + m[4]*c[1] + m[8]*c[2] + m[12],
			m[1]*c[0] + m[5]*c[1] + m[9]*c[2] + m[13],
			m[2]*c[0] + m[6]*c[1] + m[10]*c[2] + m[14],
		}
	}
	median := func(axis int) float32 {
		v := make([]float32, n)
		for i, c := range centers {
			v[i] = c[axis]
		}
		sortFloat32(v)
		return v[n/2]
	}
	center = glm.Vec3f{median(0), median(1), median(2)}
	dists := make([]float32, n)
	for i, c := range centers {
		dx, dy, dz := c[0]-center[0], c[1]-center[1], c[2]-center[2]
		dists[i] = float32(math.Sqrt(float64(dx*dx + dy*dy + dz*dz)))
	}
	sortFloat32(dists)
	k := int(float32(n-1) * clamp01(pct))
	radius = dists[k]
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

func sortFloat32(v []float32) {
	for i := 1; i < len(v); i++ {
		x := v[i]
		j := i - 1
		for j >= 0 && v[j] > x {
			v[j+1] = v[j]
			j--
		}
		v[j+1] = x
	}
}

// Destroy releases the scene's GPU buffers, lights and mesh resource references.
func (s *Scene) Destroy() {
	for i := range s.meshes {
		s.meshes[i].geometry.Release()
		s.meshes[i].material.Release()
	}
	s.meshes = nil
	if s.drawList != nil {
		s.drawList.destroy()
	}
	if s.lights != nil {
		s.lights.Destroy()
	}
}

// FrustumPlanes extracts the 6 normalized inward frustum planes (Gribb-Hartmann;
// near = row2 for Vulkan/glm 0..1 clip depth) from a column-major view-projection.
func FrustumPlanes(vp glm.Mat4f) [6][4]float32 {
	row := func(i int) [4]float32 { return [4]float32{vp[i], vp[4+i], vp[8+i], vp[12+i]} }
	r0, r1, r2, r3 := row(0), row(1), row(2), row(3)
	comb := func(a, b [4]float32, sgn float32) [4]float32 {
		return [4]float32{a[0] + sgn*b[0], a[1] + sgn*b[1], a[2] + sgn*b[2], a[3] + sgn*b[3]}
	}
	pl := [6][4]float32{comb(r3, r0, 1), comb(r3, r0, -1), comb(r3, r1, 1), comb(r3, r1, -1), r2, comb(r3, r2, -1)}
	for i := range pl {
		p := pl[i]
		l := float32(math.Sqrt(float64(p[0]*p[0] + p[1]*p[1] + p[2]*p[2])))
		if l > 0 {
			pl[i] = [4]float32{p[0] / l, p[1] / l, p[2] / l, p[3] / l}
		}
	}
	return pl
}
