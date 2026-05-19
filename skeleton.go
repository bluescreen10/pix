package pix

import "github.com/bluescreen10/pix/glm"

// Bone is a scene node that participates in skeletal animation.
// It is a plain scene node (transforms driven by the animation system)
// with no special rendering behaviour of its own.
type Bone struct{ Node }

func (s *Scene) NewBone() Bone {
	id := s.allocNode(KindBone)
	return Bone{Node{scene: s, id: id}}
}

// Skeleton holds the bones and inverse-bind matrices for one armature.
// GPU resources (bone matrix buffer + bind group) are managed by the Renderer.
type Skeleton struct {
	bones        []Bone
	invBindMats  []glm.Mat4f // inverse world matrix of each bone at bind time
	boneMatrices []glm.Mat4f // scratch: world * invBind, uploaded to GPU each frame
}

// NewSkeleton creates a skeleton from an ordered list of bones.
func NewSkeleton(bones []Bone) *Skeleton {
	n := len(bones)
	return &Skeleton{
		bones:        append([]Bone(nil), bones...),
		invBindMats:  make([]glm.Mat4f, n),
		boneMatrices: make([]glm.Mat4f, n),
	}
}

// Bind captures the current world transform of each bone as the bind pose.
// Call this after positioning the skeleton in its rest pose, before any animation.
func (sk *Skeleton) Bind(scene *Scene) {
	for i, b := range sk.bones {
		sk.invBindMats[i] = scene.world[b.slot()].Inv()
	}
}

// BoneCount returns the number of bones in the skeleton.
func (sk *Skeleton) BoneCount() int { return len(sk.bones) }

// update recomputes boneMatrices from the current bone world transforms.
// Called by the renderer each frame before uploading to the GPU.
func (sk *Skeleton) update(scene *Scene) {
	for i, b := range sk.bones {
		sk.boneMatrices[i] = scene.world[b.slot()].Mul4x4(sk.invBindMats[i])
	}
}
