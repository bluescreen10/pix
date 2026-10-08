package scenes

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
// this field owns. lods/hysteresis/lodGroupID are shared by every instance — an
// InstancedMesh's LOD levels are a property of the field as a whole, not of any one
// instance; only the transform differs per instance (see AddLOD).
type instancedMeshData struct {
	lods       []lodLevel
	hysteresis float32
	bounds     glm.Sphere

	transformBase uint32 // offset into Scene.instanceTransforms
	count         uint32

	ownerNode uint32

	// Every instance shares one set of morph weights and so one deformOutput — see
	// meshData for both, and for meshPacketIndex.
	morph           morphState
	deformOutput    geometries.Geometry
	meshPacketIndex uint32
}

func (m InstancedMesh) data() *instancedMeshData {
	return &m.scene.instancedMeshes[m.scene.payload[m.slot()]]
}

// Geometry returns the field's (level-0) geometry handle.
func (m InstancedMesh) Geometry() geometries.Geometry {
	return m.data().lods[0].geometry
}

// Material returns the field's (level-0) material handle.
func (m InstancedMesh) Material() materials.Material {
	return m.data().lods[0].material
}

// Count returns the number of instances.
func (m InstancedMesh) Count() int {
	return int(m.data().count)
}

// AddLOD appends a coarser level shared by every instance in this field — see
// Mesh.AddLOD's doc comment; the same rules (increasing minDistance, shared bounds,
// at most maxLODLevels) apply here. Every instance gets its own individually culled
// and LOD-selected record per level (see Scene.collectDrawables): a 3-level field of N
// instances contributes 3N drawable records, not N — a real cost for very large N (tens
// of thousands, e.g. a uniform grass field), so this is aimed at lower-count instanced
// content (rocks, trees, props) rather than the grass-blade-count case.
func (m InstancedMesh) AddLOD(geo geometries.Geometry, mat materials.Material, minDistance float32) InstancedMesh {
	md := m.data()
	md.lods = appendLODLevel(md.lods, geo, mat, minDistance, md.deformOutput)
	m.scene.packetDirty = true
	return m
}

// SetLODHysteresis sets the sticky band (world units) shared by every instance in this
// field — see Mesh.SetLODHysteresis.
func (m InstancedMesh) SetLODHysteresis(h float32) InstancedMesh {
	md := m.data()
	md.hysteresis = h
	if len(md.lods) > 1 {
		m.scene.packetDirty = true
	}
	return m
}

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

	id := s.allocNode(kindInstancedMesh)
	payloadIdx := uint32(len(s.instancedMeshes))
	transformBase := uint32(len(s.instanceTransforms))
	s.instanceTransforms = append(s.instanceTransforms, transforms...)
	s.instancedMeshes = append(s.instancedMeshes, instancedMeshData{
		lods:            []lodLevel{{geometry: geo.Copy(), material: mat.Copy()}},
		bounds:          geo.BoundingSphere(),
		transformBase:   transformBase,
		count:           uint32(len(transforms)),
		ownerNode:       id.index,
		morph:           newMorphState(geo),
		deformOutput:    newMorphOutput(geo),
		meshPacketIndex: invalidIndex,
	})
	s.payload[id.index] = payloadIdx
	s.packetDirty = true
	return InstancedMesh{Node{scene: s, id: id}}
}

func (s *Scene) swapRemoveInstancedMesh(payloadIdx uint32) {
	d := &s.instancedMeshes[payloadIdx]
	releaseLODs(d.lods)
	d.deformOutput.Release()
	last := uint32(len(s.instancedMeshes) - 1)
	if payloadIdx != last {
		s.instancedMeshes[payloadIdx] = s.instancedMeshes[last]
		s.payload[s.instancedMeshes[payloadIdx].ownerNode] = payloadIdx
	}
	s.instancedMeshes = s.instancedMeshes[:last]
}
