package pix

import (
	"unsafe"

	"github.com/bluescreen10/pix/glm"
	"github.com/bluescreen10/pix/gpu"
	"github.com/bluescreen10/pix/internal/mem"
)

// jointElemSize is the byte size of one joint matrix (mat4) in the scene's joint
// buffer, used to turn a TLSF byte offset into the joint-index base stored on
// gpuDrawable/skinCmd.
const jointElemSize uint32 = 64

const initialJointBytes uint32 = 64 * 64 // room for 64 joints before the first grow

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
// crude per-joint scale estimate), recomputed in syncSkinning and reused by every
// SkinnedMesh sharing this skeleton for their bounds (see skinned_mesh.go).
type skeletonData struct {
	bones      []NodeID
	names      []string
	invBind    []glm.Mat4f
	bindPose   []Transform
	jointAlloc mem.Allocation
	jointBase  uint32
	ownerNode  uint32

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
	return s.scene.skeletons.Get(s.scene.payload[s.slot()])
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
		s.scene.flags[id.index] |= flagDirty
	}
}

// NewSkeleton builds a bone hierarchy from cfg: one KindBone node per joint,
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

	rootID := s.allocNode(KindSkeleton)
	bones := make([]NodeID, n)
	for i := 0; i < n; i++ {
		p := cfg.Parents[i]
		if p >= int32(i) {
			panic("pix: SkeletonConfig.Parents[i] must be < i (topological order)")
		}
		id := s.allocNode(KindBone)
		if p < 0 {
			s.reparent(id, rootID)
		} else {
			s.reparent(id, bones[p])
		}
		s.transforms[id.index] = cfg.BindPose[i]
		s.flags[id.index] |= flagDirty
		bones[i] = id
	}

	invBind := append([]glm.Mat4f(nil), cfg.InverseBind...)
	bindPose := append([]Transform(nil), cfg.BindPose...)
	alloc := s.allocJoints(uint32(n))

	payloadIdx, _ := s.skeletons.Alloc(skeletonData{
		bones: bones, names: names, invBind: invBind, bindPose: bindPose,
		jointAlloc: alloc, jointBase: alloc.Offset() / jointElemSize, ownerNode: rootID.index,
	})
	s.payload[rootID.index] = payloadIdx
	return Skeleton{Node{scene: s, id: rootID}}
}

func (s *Scene) freeSkeleton(payloadIdx uint32) {
	sk := s.skeletons.Get(payloadIdx)
	if s.jointTLSF != nil {
		s.jointTLSF.Free(sk.jointAlloc)
	}
	s.skeletons.Free(payloadIdx)
}

// allocJoints suballocates n joints' worth of space in the scene's joint buffer,
// growing it (and re-suballocating every live skeleton) if needed.
func (s *Scene) allocJoints(n uint32) mem.Allocation {
	need := n * jointElemSize
	if s.jointTLSF == nil {
		initCap := initialJointBytes
		for initCap < need {
			initCap *= 2
		}
		s.jointTLSF = mem.NewTLSF(initCap)
		s.jointBuf = s.backend.Alloc(uint64(initCap), gpu.MemoryHost, "joints")
	}
	if alloc, err := s.jointTLSF.Alloc(need); err == nil {
		return alloc
	}
	free, _ := s.jointTLSF.StorageReport()
	used := s.jointTLSF.Capacity() - free
	s.growJoints(used + need)
	alloc, err := s.jointTLSF.Alloc(need)
	if err != nil {
		panic("pix: joint buffer alloc failed after grow")
	}
	return alloc
}

// growJoints replaces the joint buffer with a larger one, re-suballocating every
// live skeleton's range at the same size. No data is copied — syncSkinning
// rewrites every joint matrix from scratch every frame regardless.
func (s *Scene) growJoints(minCap uint32) {
	newCap := s.jointTLSF.Capacity()
	if newCap == 0 {
		newCap = initialJointBytes
	}
	for newCap < minCap {
		newCap *= 2
	}
	newTLSF := mem.NewTLSF(newCap)
	for _, sk := range s.skeletons.All() {
		alloc, err := newTLSF.Alloc(sk.jointAlloc.Size())
		if err != nil {
			panic("pix: joint buffer repack failed")
		}
		sk.jointAlloc = alloc
		sk.jointBase = alloc.Offset() / jointElemSize
	}
	if s.jointBuf.Valid() {
		s.backend.Free(s.jointBuf)
	}
	s.jointBuf = s.backend.Alloc(uint64(newCap), gpu.MemoryHost, "joints")
	s.jointTLSF = newTLSF
}

// syncSkinning recomputes every skeleton's joint matrices and per-joint scratch
// (skeleton-local bone positions + scale) from the just-updated world transforms,
// then each SkinnedMesh's world-pose bounding sphere from that scratch (see
// skinned_mesh.go). Joints are written directly into the joint buffer (MemoryHost,
// no staging) in skeleton-local space: rootWorldInv * boneWorld * invBind — so a
// SkinnedMesh's drawable, whose transformID is the skeleton root, applies the
// remaining world transform exactly like static geometry.
func (s *Scene) syncSkinning() {
	if s.skeletons.Len() == 0 {
		return
	}
	for _, sk := range s.skeletons.All() {
		rootInv := s.worldInv[sk.ownerNode]
		n := len(sk.bones)
		if cap(sk.jointPos) < n {
			sk.jointPos = make([]glm.Vec3f, n)
			sk.jointScale = make([]float32, n)
		}
		sk.jointPos = sk.jointPos[:n]
		sk.jointScale = sk.jointScale[:n]
		joints := unsafe.Slice((*glm.Mat4f)(unsafe.Add(s.jointBuf.Ptr, uintptr(sk.jointAlloc.Offset()))), n)
		for j, id := range sk.bones {
			rl := rootInv.Mul4x4(s.world[id.index])
			joints[j] = rl.Mul4x4(sk.invBind[j])
			sk.jointPos[j] = glm.Vec3f{rl[12], rl[13], rl[14]}
			sk.jointScale[j] = maxColumnLength(rl)
		}
	}
	for _, sm := range s.skinnedMeshes.All() {
		sk := s.skeletons.Get(sm.skeleton)
		sm.bounds = skinnedBounds(sk.jointPos, sk.jointScale, sm.radii)
	}
}

// maxColumnLength estimates a matrix's largest axis scale (mirrors scene_cull.comp's
// bounds-transform approximation), used to scale a joint's precomputed bind-space
// radius by its current pose.
func maxColumnLength(m glm.Mat4f) float32 {
	col := func(base int) float32 {
		x, y, z := m[base], m[base+1], m[base+2]
		return sqrt32(x*x + y*y + z*z)
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
