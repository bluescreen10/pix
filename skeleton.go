package pix

import (
	"github.com/bluescreen10/dawn-go/wgpu"
	"github.com/bluescreen10/pix/glm"
)

//TODO: bone updates should happen via skeleton so we eliminate the need for sk.update

// SkeletonData is the renderer-owned resource for one armature.
// GPU buffer and bind group are allocated lazily by syncSkeletons.
type SkeletonData struct {
	bones        []Bone
	invBindMats  []glm.Mat4f
	boneMatrices []glm.Mat4f // scratch: meshLocalBone = meshWorldInv * boneWorld * invBind
	gpuBuf       *wgpu.Buffer
	bindGroup    *wgpu.BindGroup

	version    int
	gpuVersion int
}

func (sd *SkeletonData) BoneCount() int { return len(sd.bones) }

func (sd *SkeletonData) Destroy() {
	if sd.bindGroup != nil {
		sd.bindGroup.Release()
		sd.bindGroup = nil
	}
	if sd.gpuBuf != nil {
		sd.gpuBuf.Destroy()
		sd.gpuBuf = nil
	}
}

func (sd *SkeletonData) update(modelInv glm.Mat4f) {
	for i, b := range sd.bones {
		sd.boneMatrices[i] = modelInv.Mul4x4(b.WorldTransform().Mul4x4(sd.invBindMats[i]))
	}
	sd.gpuVersion = sd.version
}

func (sd *SkeletonData) NeedsUpdate() {
	sd.version++
}

// Skeleton is the public handle for a renderer-owned skeleton resource.
type Skeleton struct {
	renderer *Renderer
	ref      Ref[Skeleton]
}

func (s Skeleton) Ref() Ref[Skeleton] { return s.ref }
func (s Skeleton) Release()           { s.ref.Release() }
func (s Skeleton) Copy() Skeleton     { return Skeleton{renderer: s.renderer, ref: s.ref.Copy()} }
func (s Skeleton) Valid() bool        { return s.renderer != nil && s.ref.Valid() }
func (s Skeleton) BoneCount() int     { return s.renderer.skeletons.get(s.ref.ID()).BoneCount() }
func (s Skeleton) NeedsUpdate() {
	s.renderer.skeletons.get(s.ref.ID()).NeedsUpdate()
}

// Bone is a scene node that participates in skeletal animation.
type Bone struct{ Node }

func (s *Scene) NewBone() Bone {
	id := s.allocNode(KindBone)
	return Bone{Node{scene: s, id: id}}
}

type BoneProxy struct {
	Bone
	skeletons []Skeleton
}

func NewBoneProxy(bone Bone) *BoneProxy {
	return &BoneProxy{Bone: bone}
}

func (p *BoneProxy) AddSkeleton(s Skeleton) {
	p.skeletons = append(p.skeletons, s)
}

func (p *BoneProxy) SetPosition(pos glm.Vec3f) {
	p.Bone.SetPosition(pos)
	for _, s := range p.skeletons {
		s.NeedsUpdate()
	}
}

func (p *BoneProxy) SetRotationQuat(quat glm.Quatf) {
	p.Bone.SetRotationQuat(quat)
	for _, s := range p.skeletons {
		s.NeedsUpdate()
	}
}

func (p *BoneProxy) SetScale(scale glm.Vec3f) {
	p.Bone.SetScale(scale)
	for _, s := range p.skeletons {
		s.NeedsUpdate()
	}
}
