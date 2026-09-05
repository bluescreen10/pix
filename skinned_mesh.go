package pix

import (
	"github.com/bluescreen10/pix/geometries"
	"github.com/bluescreen10/pix/glm"
	"github.com/bluescreen10/pix/materials"
)

// skinWeightEpsilon is the minimum blend weight that counts as "this joint
// influences this vertex" — below it, floating-point noise in an authored weight
// shouldn't make a joint contribute to computeJointRadii's bind-space radius.
const skinWeightEpsilon = 1e-4

// skinnedMeshData is the per-SkinnedMesh payload. srcGeometry is kept alive (a ref)
// because it carries the CPU-side skin index/weight bytes the compute pass reads
// every frame (via its descriptor, not through this handle); outputGeo is the
// derived, compute-filled geometry that actually gets drawn. radii[j] is joint j's
// bind-space influence radius (negative = joint unused by this mesh — see
// computeJointRadii); bounds is recomputed every Sync in skeleton-local space.
type skinnedMeshData struct {
	srcGeometry geometries.Geometry
	outputGeo   geometries.Geometry
	material    materials.Material
	skeleton    uint32 // slab id into Scene.skeletons
	vertCount   uint32
	radii       []float32
	bounds      glm.Sphere
	ownerNode   uint32
}

// SkinnedMesh is a typed node handle for a compute-skinned mesh. It embeds Node for
// hierarchy/visibility/shadow-flag methods, but its own local transform plays no
// part in rendering — move the Skeleton to move the character (see skeleton.go).
type SkinnedMesh struct{ Node }

func (m SkinnedMesh) data() *skinnedMeshData {
	return m.scene.skinnedMeshes.Get(m.scene.payload[m.slot()])
}

// SourceGeometry returns the mesh's un-skinned source geometry (the one carrying
// skin index/weight attributes).
func (m SkinnedMesh) SourceGeometry() geometries.Geometry { return m.data().srcGeometry }

// Material returns the mesh's material handle.
func (m SkinnedMesh) Material() materials.Material { return m.data().material }

// SetMaterial swaps the mesh's material (the cached materialID changes, so the
// scene's drawables are rebuilt).
func (m SkinnedMesh) SetMaterial(mat materials.Material) {
	md := m.data()
	newRef := mat.Copy()
	md.material.Release()
	md.material = newRef
	m.scene.drawableDirty = true
}

// Skeleton returns the skeleton this mesh is bound to.
func (m SkinnedMesh) Skeleton() Skeleton {
	root := m.scene.skeletons.Get(m.data().skeleton).ownerNode
	id := NodeID{index: root, gen: m.scene.generation[root]}
	return Skeleton{Node{scene: m.scene, id: id}}
}

// BoundingSphere returns the mesh's current skeleton-local bounding sphere (valid
// after Sync — i.e. after the first Render call following any pose change).
func (m SkinnedMesh) BoundingSphere() glm.Sphere { return m.data().bounds }

// NewSkinnedMesh creates a compute-skinned mesh bound to skel: geo must carry
// AttributeSkinIndex/AttributeSkinWeight (indices relative to skel's joint order).
// The renderer allocates geo a persistent compute-skinning output range (a derived
// geometry, sized to geo's vertex count, reusing geo's index range) that is
// refilled every frame from skel's current pose — see Renderer.dispatchSkinning.
// The scene takes its own references to geo/mat (Copy), so the caller may Release
// theirs. Destroy every SkinnedMesh bound to a Skeleton before destroying the
// Skeleton itself — a SkinnedMesh does not hold a reference on its skeleton, so
// destroying the skeleton first leaves it pointing at a freed slot.
func (s *Scene) NewSkinnedMesh(geo geometries.Geometry, mat materials.Material, skel Skeleton) SkinnedMesh {
	s.validate(skel.id)
	if s.kind[skel.slot()] != KindSkeleton {
		panic("pix: NewSkinnedMesh requires a Skeleton")
	}
	skelIdx := s.payload[skel.slot()]
	sk := s.skeletons.Get(skelIdx)

	positions := geo.GetAttributeData[glm.Vec3f](geometries.AttributePosition)
	radii, bindPos := computeJointRadii(geo, sk.invBind, positions)
	unitScale := make([]float32, len(bindPos))
	for i := range unitScale {
		unitScale[i] = 1
	}

	id := s.allocNode(KindSkinnedMesh)
	payloadIdx, _ := s.skinnedMeshes.Alloc(skinnedMeshData{
		srcGeometry: geo.Copy(),
		outputGeo:   geo.SkinOutput(),
		material:    mat.Copy(),
		skeleton:    skelIdx,
		vertCount:   uint32(len(positions)),
		radii:       radii,
		// Seeded from the bind pose so FrameSphere/BoundingSphere are sane before
		// the first Sync (which is when a pose-driven bounds would first exist).
		bounds:    skinnedBounds(bindPos, unitScale, radii),
		ownerNode: id.index,
	})
	s.payload[id.index] = payloadIdx
	s.drawableDirty = true
	return SkinnedMesh{Node{scene: s, id: id}}
}

func (s *Scene) freeSkinnedMesh(payloadIdx uint32) {
	sm := s.skinnedMeshes.Get(payloadIdx)
	sm.srcGeometry.Release()
	sm.outputGeo.Release()
	sm.material.Release()
	s.skinnedMeshes.Free(payloadIdx)
}

// computeJointRadii computes, for each joint in invBind, the farthest bind-pose
// vertex position it influences (weight > skinWeightEpsilon) from that joint's own
// bind-pose position (also returned, as bindPos) — one O(vertices × 4) pass over
// the geometry's skin data. A joint radii[j] stays negative if no vertex ever
// weights it (skinnedBounds skips those when building the mesh's bounding sphere).
func computeJointRadii(geo geometries.Geometry, invBind []glm.Mat4f, positions []glm.Vec3f) (radii []float32, bindPos []glm.Vec3f) {
	joints := geo.GetAttributeData[glm.Vec4[uint16]](geometries.AttributeSkinIndex)
	weights := geo.GetAttributeData[glm.Vec4f](geometries.AttributeSkinWeight)
	if joints == nil || weights == nil {
		panic("pix: NewSkinnedMesh requires geometry with skin index and skin weight attributes")
	}

	bindPos = make([]glm.Vec3f, len(invBind))
	for i, ib := range invBind {
		m := ib.Inv()
		bindPos[i] = glm.Vec3f{m[12], m[13], m[14]}
	}

	radii = make([]float32, len(invBind))
	for i := range radii {
		radii[i] = -1
	}
	for v, p := range positions {
		j, w := joints[v], weights[v]
		for k := 0; k < 4; k++ {
			if w[k] <= skinWeightEpsilon {
				continue
			}
			ji := int(j[k])
			if ji < 0 || ji >= len(radii) {
				continue
			}
			d := p.Sub(bindPos[ji]).Length()
			if radii[ji] < 0 || d > radii[ji] {
				radii[ji] = d
			}
		}
	}
	return radii, bindPos
}
