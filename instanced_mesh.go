package pix

import (
	"github.com/bluescreen10/pix/geometries"
	"github.com/bluescreen10/pix/glm"
	"github.com/bluescreen10/pix/materials"
)

// InstancedMesh is a typed node handle for many static instances of one geometry +
// material, batched into real GPU-instanced draws through the same culling/batching
// pipeline meshes already use (see Scene.collectDrawables and drawList.rebuild — a
// gpuDrawable.transformID is already just a flat index into an array of mat4, so
// instancing needed no shader changes, only a place to put the transforms; see
// Scene.instanceTransforms). It embeds Node, so hierarchy and transform methods are
// available, though most have no effect here — instance placement is baked into
// InstanceCount transforms given at construction, not the node's own transform.
type InstancedMesh struct{ Node }

// instancedMeshData is the per-field payload stored in Scene.instancedMeshes,
// mirroring meshData's shape (mesh.go) plus the slice of Scene.instanceTransforms
// this field owns.
type instancedMeshData struct {
	geometry geometries.Geometry
	material materials.Material
	bounds   glm.Sphere

	transformBase uint32 // offset into Scene.instanceTransforms
	count         uint32

	ownerNode uint32
}

func (m InstancedMesh) data() *instancedMeshData {
	return &m.scene.instancedMeshes[m.scene.payload[m.slot()]]
}

// Geometry returns the field's geometry handle.
func (m InstancedMesh) Geometry() geometries.Geometry { return m.data().geometry }

// Material returns the field's material handle.
func (m InstancedMesh) Material() materials.Material { return m.data().material }

// Count returns the number of instances.
func (m InstancedMesh) Count() int { return int(m.data().count) }

// NewInstancedMesh creates an InstancedMesh from a geometry + material (both
// renderer-owned; the scene takes its own references, Copy, so the caller may
// Release theirs) and one world transform per instance. transforms is copied once,
// at construction — there is no per-instance update after creation in this version,
// and no reclaiming of the Scene.instanceTransforms space an InstancedMesh used once
// destroyed: instances are meant for static, long-lived content (a rock field, a
// crowd, grass — see examples/grass), not something created and destroyed per frame.
func (s *Scene) NewInstancedMesh(geo geometries.Geometry, mat materials.Material, transforms []glm.Mat4f) InstancedMesh {
	if len(transforms) == 0 {
		panic("pix: NewInstancedMesh requires at least one transform")
	}

	id := s.allocNode(KindInstancedMesh)
	payloadIdx := uint32(len(s.instancedMeshes))
	transformBase := uint32(len(s.instanceTransforms))
	s.instanceTransforms = append(s.instanceTransforms, transforms...)
	s.instancedMeshes = append(s.instancedMeshes, instancedMeshData{
		geometry:      geo.Copy(),
		material:      mat.Copy(),
		bounds:        geo.BoundingSphere(),
		transformBase: transformBase,
		count:         uint32(len(transforms)),
		ownerNode:     id.index,
	})
	s.payload[id.index] = payloadIdx
	s.drawableDirty = true
	return InstancedMesh{Node{scene: s, id: id}}
}

func (s *Scene) swapRemoveInstancedMesh(payloadIdx uint32) {
	d := &s.instancedMeshes[payloadIdx]
	d.geometry.Release()
	d.material.Release()
	last := uint32(len(s.instancedMeshes) - 1)
	if payloadIdx != last {
		s.instancedMeshes[payloadIdx] = s.instancedMeshes[last]
		s.payload[s.instancedMeshes[payloadIdx].ownerNode] = payloadIdx
	}
	s.instancedMeshes = s.instancedMeshes[:last]
}
