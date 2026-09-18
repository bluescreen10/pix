package scenes

import (
	"slices"

	"github.com/bluescreen10/pix/glm"
	"github.com/chewxy/math32"
)

// SkeletonConfig is a skeleton's bind-time data: parallel arrays indexed by joint.
// Parents[i] must be < i (topological order; a root joint's parent is -1).
// InverseBind maps a bind-pose vertex into joint i's local space, in skeleton-root
// space (the convention glTF and most DCC exporters use). Names may be nil or
// shorter than the joint count — unnamed joints simply don't resolve by name.
type SkeletonConfig struct {
	Names       []string
	Parents     []int32
	InverseBind []glm.Mat4f
	BindPose    []Transform
}

// skeletonData is the per-skeleton payload: bones[i] is joint i's scene node.
// jointPos/jointScale are per-frame scratch (skeleton-local bone positions + a
// crude per-joint scale estimate), recomputed in updateSkinning and reused by every
// SkinnedMesh sharing this skeleton for their bounds (see skinned_mesh.go).
type skeletonData struct {
	bones    []NodeID
	names    []string
	invBind  []glm.Mat4f
	bindPose []Transform
	// jointBase is this skeleton's offset into the scene's flat joint table, assigned
	// by each updateSkinning as it lays the skeletons out one after another. There is no
	// allocator: the whole table is rewritten every frame, so a stable address would
	// buy nothing and cost a suballocator's worth of machinery.
	jointBase uint32
	ownerNode uint32

	jointPos   []glm.Vec3f
	jointScale []float32
}

// Skeleton is a typed node handle: the root of a bone hierarchy, and the space
// compute-skinned vertex output is written in (see skinned_mesh.go). Move the
// skeleton to move the character — a SkinnedMesh's own local transform is not used
// for rendering.
type Skeleton struct{ Node }

// Bone is a typed node handle for one joint in a skeleton's hierarchy. An ordinary
// scene node: parent other nodes to it (Bone.Add) to attach props that follow it.
type Bone struct{ Node }

func (s Skeleton) data() *skeletonData {
	return s.scene.skeletons.Value(s.scene.payload[s.slot()])
}

// Bone returns joint i's node handle.
func (s Skeleton) Bone(index int) Bone {
	return Bone{Node{scene: s.scene, id: s.data().bones[index]}}
}

// BoneByName returns the named joint's node handle, or the zero Bone if not found.
func (s Skeleton) BoneByName(name string) Bone {
	d := s.data()
	for i, n := range d.names {
		if n == name {
			return s.Bone(i)
		}
	}
	return Bone{}
}

// BoneCount returns the number of joints.
func (s Skeleton) BoneCount() int {
	return len(s.data().bones)
}

// Pose resets every joint to its bind-pose local transform.
func (s Skeleton) Pose() {
	d := s.data()
	for i, id := range d.bones {
		s.scene.transforms[id.index] = d.bindPose[i]
		s.scene.flags[id.index] |= flagTransformDirty
	}
}

// NewSkeleton builds a bone hierarchy from cfg: one bone node per joint,
// parented per cfg.Parents, and allocates the skeleton's range in the scene's
// joint-matrix buffer. The returned Skeleton is the root of that hierarchy.
func (s *Scene) NewSkeleton(cfg SkeletonConfig) Skeleton {
	n := len(cfg.Parents)
	if n == 0 || len(cfg.InverseBind) != n || len(cfg.BindPose) != n {
		panic("pix: SkeletonConfig.Parents/InverseBind/BindPose must have equal, nonzero length")
	}
	names := cfg.Names
	if len(names) != n {
		names = make([]string, n)
	}

	rootID := s.allocNode(kindSkeleton)
	bones := make([]NodeID, n)
	for i := range n {
		p := cfg.Parents[i]
		if p >= int32(i) {
			panic("pix: SkeletonConfig.Parents[i] must be < i (topological order)")
		}
		id := s.allocNode(kindBone)
		if p < 0 {
			s.reparent(id, rootID)
		} else {
			s.reparent(id, bones[p])
		}
		s.transforms[id.index] = cfg.BindPose[i]
		s.flags[id.index] |= flagTransformDirty
		s.names[id.index] = names[i]
		bones[i] = id
	}

	invBind := append([]glm.Mat4f(nil), cfg.InverseBind...)
	bindPose := append([]Transform(nil), cfg.BindPose...)
	payloadIdx, _ := s.skeletons.Alloc(skeletonData{
		bones: bones, names: names, invBind: invBind, bindPose: bindPose,
		ownerNode: rootID.index,
	})
	s.payload[rootID.index] = payloadIdx
	return Skeleton{Node{scene: s, id: rootID}}
}

func (s *Scene) freeSkeleton(payloadIdx uint32) {
	s.skeletons.Free(payloadIdx)
}

// updateSkinning recomputes every skeleton's joint matrices and per-joint scratch
// (skeleton-local bone positions + scale) from the just-updated world transforms,
// then each SkinnedMesh's world-pose bounding sphere from that scratch (see
// skinned_mesh.go). Joints are written directly into the joint buffer (MemoryHost,
// no staging) in skeleton-local space: rootWorldInv * boneWorld * invBind — so a
// SkinnedMesh's drawable, whose transformID is the skeleton root, applies the
// remaining world transform exactly like static geometry.
func (s *Scene) updateSkinning() {
	s.packet.Joints.Data = s.packet.Joints.Data[:0]
	if s.skeletons.Len() == 0 {
		return
	}
	for _, sk := range s.skeletons.Entries() {
		rootInv := s.world[sk.ownerNode].Inv()
		n := len(sk.bones)
		if cap(sk.jointPos) < n {
			sk.jointPos = make([]glm.Vec3f, n)
			sk.jointScale = make([]float32, n)
		}
		sk.jointPos = sk.jointPos[:n]
		sk.jointScale = sk.jointScale[:n]
		sk.jointBase = uint32(len(s.packet.Joints.Data))
		// Grow-then-reslice rather than append-a-temporary: the table keeps its capacity
		// across frames, so this allocates only while a scene is still growing.
		s.packet.Joints.Data = slices.Grow(s.packet.Joints.Data, n)[:int(sk.jointBase)+n]
		joints := s.packet.Joints.Data[sk.jointBase:]
		for j, id := range sk.bones {
			rl := rootInv.Mul4x4(s.world[id.index])
			joints[j] = rl.Mul4x4(sk.invBind[j])
			sk.jointPos[j] = glm.Vec3f{rl[12], rl[13], rl[14]}
			sk.jointScale[j] = maxColumnLength(rl)
		}
	}
	for _, sm := range s.skinnedMeshes.Entries() {
		sk := s.skeletons.Value(sm.skeleton)
		sm.bounds = skinnedBounds(sk.jointPos, sk.jointScale, sm.radii)
	}
}

// maxColumnLength estimates a matrix's largest axis scale (mirrors scene_cull.comp's
// bounds-transform approximation), used to scale a joint's precomputed bind-space
// radius by its current pose.
func maxColumnLength(m glm.Mat4f) float32 {
	col := func(base int) float32 {
		x, y, z := m[base], m[base+1], m[base+2]
		return math32.Sqrt(x*x + y*y + z*z)
	}
	a, b, c := col(0), col(4), col(8)
	if b > a {
		a = b
	}
	if c > a {
		a = c
	}
	return a
}

// skinnedBounds computes a skeleton-local bounding sphere for one SkinnedMesh's
// current pose: center is the mean position of joints it actually uses (radii[i] <
// 0 marks an unused joint — see computeJointRadii), radius covers every used
// joint's current position plus its bind-space influence radius scaled by the
// joint's current pose scale.
func skinnedBounds(pos []glm.Vec3f, scale []float32, radii []float32) glm.Sphere {
	var center glm.Vec3f
	n := 0
	for i, r := range radii {
		if r < 0 || i >= len(pos) {
			continue
		}
		center = center.Add(pos[i])
		n++
	}
	if n == 0 {
		return glm.Sphere{Radius: 0.01}
	}
	center = center.Scale(1.0 / float32(n))
	var radius float32
	for i, r := range radii {
		if r < 0 || i >= len(pos) {
			continue
		}
		d := pos[i].Sub(center).Length() + r*scale[i]
		if d > radius {
			radius = d
		}
	}
	return glm.Sphere{Center: center, Radius: radius}
}
