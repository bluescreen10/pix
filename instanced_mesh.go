package pix

import (
	"github.com/bluescreen10/dawn-go/wgpu"
	"github.com/bluescreen10/pix/glm"
)

// InstancedMesh is a scene node that renders N copies of the same geometry and
// material in a single draw call. Each instance has its own local transform set
// via SetMatrixAt; the final world transform is the node's world matrix × that
// local matrix.
type InstancedMesh struct{ Node }

type instancedMeshData struct {
	geometry      Geometry
	material      Material
	ownerNode     uint32
	instanceCount int
	pipelines     [numPipelineTypes]*wgpu.RenderPipeline
}

func (m InstancedMesh) data() *instancedMeshData {
	return &m.scene.instancedMeshes[m.scene.payload[m.slot()]]
}

// Count returns the number of instances.
func (m InstancedMesh) Count() int { return m.data().instanceCount }

// SetMatrixAt sets the local transform for instance i and marks it dirty.
func (m InstancedMesh) SetMatrixAt(i int, mat glm.Mat4f) {
	firstChild := m.scene.firstChildren[m.slot()]
	childIdx := firstChild.index + uint32(i)
	m.scene.local[childIdx] = mat
	m.scene.flags[childIdx] |= flagDirty
}

// MatrixAt returns the local transform for instance i.
func (m InstancedMesh) MatrixAt(i int) glm.Mat4f {
	firstChild := m.scene.firstChildren[m.slot()]
	return m.scene.local[firstChild.index+uint32(i)]
}

// NewInstancedMesh creates an instanced mesh node with count instances, all
// initially set to the identity transform.
func (s *Scene) NewInstancedMesh(geo Geometry, mat Material, count int) InstancedMesh {
	id := s.allocMultiNode(KindInstancedMesh, KindInstance, count)
	payloadIdx := uint32(len(s.instancedMeshes))

	s.instancedMeshes = append(s.instancedMeshes, instancedMeshData{
		geometry:      geo.Copy(),
		material:      mat.Copy(),
		ownerNode:     id.index,
		instanceCount: count,
	})
	s.payload[id.index] = payloadIdx
	return InstancedMesh{Node{scene: s, id: id}}
}
