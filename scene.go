package pix

import (
	"math"

	"github.com/bluescreen10/pix/colors"
	"github.com/bluescreen10/pix/glm"
	"github.com/bluescreen10/pix/gpu"
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

	// Skinning: skeletons (bone hierarchies + inverse binds) and skinned meshes
	// (a source geometry + a compute-derived output geometry, bound to a skeleton).
	// Both use a Slab rather than swap-remove: skinnedMeshData.skeleton stores a
	// skeletons slab id, which a swap-remove would silently invalidate.
	skeletons     mem.Slab[skeletonData]
	skinnedMeshes mem.Slab[skinnedMeshData]

	// jointBuf holds every skeleton's current joint matrices (skeleton-local space),
	// TLSF-suballocated per skeleton. Rewritten in full every Sync — a grow only
	// re-suballocates each skeleton's existing range (no data copy needed).
	jointBuf  gpu.Buffer
	jointTLSF *mem.TLSF

	// Light objects the scene owns; the flat GPU table (lights) is derived from them
	// (+ ambient) each frame in Sync.
	ambient     colors.RGB32F
	fog         Fog
	dirLights   []*DirectionalLight
	pointLights []*PointLight
	spotLights  []*SpotLight
	lights      *Lights

	drawableDirty bool

	// shadowsEnabled mirrors the renderer's global shadow toggle, written by the
	// renderer each frame before Sync (it owns the toggle; the light table is what
	// consumes it). False on a Scene synced without a renderer, which is the safe
	// reading: publish no shadow maps rather than stale ones.
	shadowsEnabled bool
	drawList       *drawList

	// FrameSphere scratch, retained because it runs every frame (see prepareShadows).
	frameCenters []glm.Vec3f
	frameReach   []float32
	frameScratch []float32
}

// NewScene creates an empty scene bound to a backend (usually via Renderer.NewScene).
func NewScene(backend gpu.Backend) *Scene {
	s := &Scene{backend: backend, freeHead: invalidIdx, topoDirty: true}
	s.skeletons = mem.NewSlab[skeletonData]()
	s.skinnedMeshes = mem.NewSlab[skinnedMeshData]()
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
func (s *Scene) SetAmbient(color colors.RGB32F) {
	s.ambient = color
}

// SetFog sets the scene's distance fog, or clears it when f is nil (the default).
// Pass one of the fog models — scene.SetFog(pix.NewExp2Fog(color, 60000)) — and keep
// the returned value if you want to animate its fields; they are re-read every frame.
func (s *Scene) SetFog(fog Fog) { s.fog = fog }

// Fog returns the scene's distance fog, or nil when there is none.
func (s *Scene) Fog() Fog { return s.fog }

// AddDirectionalLight adds a directional light (dir = travel direction) and returns
// its handle — configure it further or call CastShadow on the returned light.
func (s *Scene) AddDirectionalLight(dir glm.Vec3f, color colors.RGB32F, intensity float32) *DirectionalLight {
	l := &DirectionalLight{Direction: dir, Color: color, Intensity: intensity}
	s.dirLights = append(s.dirLights, l)
	return l
}

// AddPointLight adds a point light at pos with linear falloff to zero at rng and
// returns its handle.
func (s *Scene) AddPointLight(pos glm.Vec3f, color colors.RGB32F, intensity, rng float32) *PointLight {
	l := &PointLight{Position: pos, Color: color, Intensity: intensity, Range: rng}
	s.pointLights = append(s.pointLights, l)
	return l
}

// AddSpotLight adds a cone light at pos aimed along dir, full inside the inner cone and
// falling to zero at half-angle angle (radians) / distance rng. penumbra (0..1) sets
// the soft-edge fraction. Returns its handle.
func (s *Scene) AddSpotLight(pos, dir glm.Vec3f, color colors.RGB32F, intensity, rng, angle, penumbra float32) *SpotLight {
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
	// Reparenting can change flagAttached for child (and everything under it) once
	// flushTopoIfDirty runs — which shifts every OTHER attached mesh's position in
	// whatever collectDrawables produces next, not just child's own. Both dirty
	// flags are unconditional here for the same reason destroyNode's is: cheap to
	// over-trigger, and a narrower "only if this specific node..." check would miss
	// the reindexing risk to unrelated meshes.
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
	s.topoDirty = true
	// See reparent's comment: detaching (whether standalone via Node.Remove, or as
	// reparent's first step) can drop child out of flagAttached, reindexing every
	// OTHER attached mesh in collectDrawables' next output.
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
	}
	s.flags[idx] &^= flagAlive
	s.generation[idx]++
	s.parents[idx] = NodeID{index: s.freeHead}
	s.freeHead = idx
	//TODO: in the future detect if drawable needs to be rebuilt
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
	for i := range s.flags {
		s.flags[i] &^= flagAttached
	}
	queue := []uint32{s.root.index}
	for len(queue) > 0 {
		idx := queue[0]
		queue = queue[1:]
		if s.flags[idx]&flagAlive == 0 {
			continue
		}
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
		//TODO: compute worldInv lazily. Every dirty node pays a full 4x4 inverse
		// here, but the only readers are skeleton roots (Scene.syncSkinning) and
		// the public Node.WorldTransformInv accessor — so in a scene of 10k static
		// boxes, 10k inversions per dirty pass are thrown away unread.
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

// Sync writes the scene's own per-scene GPU state: the world matrices (recomputed
// from dirty transforms), skinning (joint matrices + bounds), and the light table.
// All of it lands in MemoryHost buffers the scene owns directly — no uploader. The
// renderer drives geometry/pipeline syncing (which do need staging, into shared
// MemoryDevice buffers) and the draw-list rebuild separately.
func (s *Scene) Sync() {
	// updateTransforms walks topoOrder, so attachment must be current first.
	s.flushTopoIfDirty()
	if dirty := s.updateTransforms(); dirty {
		s.drawList.sync(s.world) // host-visible, direct write (not staged)
	}
	s.syncSkinning()
	// Light objects are mutable, so re-derive the flat GPU table each frame; rebuild
	// only marks the buffer dirty (→ re-writes) when the derived table changed. Both
	// this and drawList.sync above write straight to MemoryHost buffers — nothing
	// scene-owned goes through the shared uploader, so a frame where nothing but
	// (say) an animated character's pose changed stages/submits nothing extra.
	s.lights.rebuild(s.ambient, s.fog, s.dirLights, s.pointLights, s.spotLights, s.shadowsEnabled)
	s.lights.Sync()
}

// collectDrawables builds the GPU drawable table from the mesh and skinned-mesh
// payloads, plus a parallel slice of each drawable's material (for batching + store
// address). A skinned mesh's transformID is its skeleton root's node slot (not its
// own) — its own local transform plays no part in rendering; see skeleton.go.
//
// Only nodes attached to the scene draw. Creating a mesh does NOT attach it —
// scene.Add (or parenting it under something attached) does. An unattached node is
// never visited by updateTransforms, so its world matrix would still be the identity
// it was born with: drawing it anyway would silently place it at the origin,
// ignoring every transform set on it. Skipping it instead makes the omission
// obvious — the mesh is simply missing until it is added.
func (s *Scene) collectDrawables() ([]gpuDrawable, []materials.Material) {
	s.flushTopoIfDirty() // attachment is derived from the topological walk
	n := len(s.meshes) + s.skinnedMeshes.Len()
	out := make([]gpuDrawable, 0, n)
	materials := make([]materials.Material, 0, n)
	for i := range s.meshes {
		md := &s.meshes[i]
		if s.flags[md.ownerNode]&flagAttached == 0 {
			continue
		}
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
			geometryID:  md.geometry.ID(),
			materialID:  md.material.ID(),
			flags:       flags,
		})
		materials = append(materials, md.material)
	}
	for _, sm := range s.skinnedMeshes.All() {
		// Both must be attached: the mesh node puts it in the scene, and the
		// skeleton root supplies the transform its drawable is rendered with.
		root := s.skeletons.Get(sm.skeleton).ownerNode
		if s.flags[sm.ownerNode]&flagAttached == 0 || s.flags[root]&flagAttached == 0 {
			continue
		}
		var flags uint32
		if s.flags[sm.ownerNode]&flagCastShadow != 0 {
			flags |= DrawableCastsShadow
		}
		if s.flags[sm.ownerNode]&flagReceiveShadow != 0 {
			flags |= DrawableReceivesShadow
		}
		out = append(out, gpuDrawable{
			bounds:      [4]float32{sm.bounds.Center[0], sm.bounds.Center[1], sm.bounds.Center[2], sm.bounds.Radius},
			transformID: root,
			geometryID:  sm.outputGeo.ID(),
			materialID:  sm.material.ID(),
			flags:       flags,
		})
		materials = append(materials, sm.material)
	}
	return out, materials
}

// MeshCount returns the number of mesh nodes in the scene.
func (s *Scene) MeshCount() int { return len(s.meshes) }

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
		s.meshes[i].geometry.Release()
		s.meshes[i].material.Release()
	}
	s.meshes = nil
	for _, sm := range s.skinnedMeshes.All() {
		sm.srcGeometry.Release()
		sm.outputGeo.Release()
		sm.material.Release()
	}
	if s.drawList != nil {
		s.drawList.destroy()
	}
	if s.lights != nil {
		s.lights.Destroy()
	}
	if s.jointBuf.Valid() {
		s.backend.Free(s.jointBuf)
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
