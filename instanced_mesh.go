package pix

import "github.com/bluescreen10/pix/glm"

// InstancedMesh is a scene node that renders N copies of the same geometry and
// material in a single draw call. Each instance has its own local transform set
// via SetMatrixAt; the final world transform is the node's world matrix × that
// local matrix.
type InstancedMesh struct{ Node }

type instancedMeshData struct {
	geometry  Geometry
	material  Material
	matrices  []glm.Mat4f // per-instance local transforms
	ownerNode uint32
}

func (m InstancedMesh) data() *instancedMeshData {
	return &m.scene.instancedMeshes[m.scene.payload[m.slot()]]
}

// Count returns the number of instances.
func (m InstancedMesh) Count() int { return len(m.data().matrices) }

// SetMatrixAt sets the local transform for instance i.
func (m InstancedMesh) SetMatrixAt(i int, mat glm.Mat4f) {
	m.data().matrices[i] = mat
}

// MatrixAt returns the local transform for instance i.
func (m InstancedMesh) MatrixAt(i int) glm.Mat4f {
	return m.data().matrices[i]
}

// NewInstancedMesh creates an instanced mesh node with count instances, all
// initially set to the identity transform.
func (s *Scene) NewInstancedMesh(geo Geometry, mat Material, count int) InstancedMesh {
	id := s.allocNode(KindInstancedMesh)
	payloadIdx := uint32(len(s.instancedMeshes))
	matrices := make([]glm.Mat4f, count)
	for i := range matrices {
		matrices[i] = glm.Mat4fIndentity
	}
	s.instancedMeshes = append(s.instancedMeshes, instancedMeshData{
		geometry:  geo.Copy(),
		material:  mat.Copy(),
		matrices:  matrices,
		ownerNode: id.index,
	})
	s.payload[id.index] = payloadIdx
	return InstancedMesh{Node{scene: s, id: id}}
}
