package pix

import "github.com/bluescreen10/dawn-go/wgpu"

// SkinnedMesh is a scene node that renders a mesh deformed by a Skeleton.
type SkinnedMesh struct{ Node }

// skinnedMeshData is the per-skinned-mesh payload stored in Scene.skinnedMeshes.
type skinnedMeshData struct {
	geometry       Geometry
	material       Material
	boundingSphere Sphere
	ownerNode      uint32
	skeleton       Skeleton
	pipelines      [numPipelineTypes]*wgpu.RenderPipeline
}

func (m SkinnedMesh) data() *skinnedMeshData {
	return &m.scene.skinnedMeshes[m.scene.payload[m.slot()]]
}

func (m SkinnedMesh) Geometry() Geometry { return m.data().geometry }
func (m SkinnedMesh) Material() Material { return m.data().material }
func (m SkinnedMesh) Skeleton() Skeleton { return m.data().skeleton }

// Bind captures the current world transform of each bone as the bind pose.
// Call after positioning the skeleton in its rest pose, before any animation.
func (m SkinnedMesh) Bind(scene *Scene) {
	sd := m.data().skeleton.renderer.skeletons.get(m.data().skeleton.ref.ID())
	for i, b := range sd.bones {
		sd.invBindMats[i] = scene.world[b.slot()].Inv()
	}
}

func (m SkinnedMesh) SetMaterial(mat Material) {
	md := m.data()
	newRef := mat.Copy()
	md.material.Release()
	md.material = newRef
	md.pipelines[PipelineGeometry] = nil
}

func (s *Scene) NewSkinnedMesh(geo Geometry, mat Material, skeleton Skeleton) SkinnedMesh {
	id := s.allocNode(KindSkinnedMesh)
	payloadIdx := uint32(len(s.skinnedMeshes))
	s.skinnedMeshes = append(s.skinnedMeshes, skinnedMeshData{
		geometry:       geo.Copy(),
		material:       mat.Copy(),
		boundingSphere: geo.BoundingSphere(),
		ownerNode:      id.index,
		skeleton:       skeleton.Copy(),
	})
	s.payload[id.index] = payloadIdx
	return SkinnedMesh{Node{scene: s, id: id}}
}
