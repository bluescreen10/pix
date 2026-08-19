package pix

// noSkeleton is the skeletonID sentinel for drawables that are not skinned.
const noSkeleton uint32 = 0xFFFFFFFF

// drawable is one draw-instance in the flattened, GPU-facing draw list: one per
// plain and skinned mesh, and one per instance of an instanced mesh. It carries
// only indices, so a single id (its position in the table) resolves both the
// transform and the geometry — the shape needed for GPU-driven vertex pulling.
//
// The layout is 4×u32 = 16 bytes for direct upload to a storage buffer.
//
//   - transformID indexes the scene world-transform arrays (the instance's node).
//   - geometryID is a placeholder until the GeometrySystem lands; for now it holds
//     the geometry resource id.
//   - skeletonID is noSkeleton unless the mesh is skinned.
//   - flags is reserved for static per-drawable bits; per-frame visibility stays
//     out of this table (it is not structural and would dirty it every frame).
type drawable struct {
	transformID uint32
	geometryID  uint32
	skeletonID  uint32
	flags       uint32
}

// rebuildDrawables flattens the mesh payload tables into the dense drawables
// list. Called lazily when drawableDirty is set — only on structural changes
// (mesh add/remove), never per frame. Removal is handled implicitly: rebuilding
// from the current payload tables simply drops the gone entries, which is what
// makes eliminating an instanced mesh's whole run trivial.
func (s *Scene) UpdateDrawables() bool {
	if !s.drawableDirty {
		return false
	}

	s.drawables = s.drawables[:0]

	for i := range s.meshes {
		md := &s.meshes[i]
		s.drawables = append(s.drawables, drawable{
			transformID: md.ownerNode,
			geometryID:  md.geometry.ref.ID(),
			skeletonID:  noSkeleton,
		})
	}

	for i := range s.skinnedMeshes {
		smd := &s.skinnedMeshes[i]
		s.drawables = append(s.drawables, drawable{
			transformID: smd.ownerNode,
			geometryID:  smd.geometry.ref.ID(),
			skeletonID:  smd.skeleton.ref.ID(),
		})
	}

	// Each instanced mesh expands to one drawable per instance. The instances are
	// consecutive child nodes, so their transforms are base .. base+count-1.
	for i := range s.instancedMeshes {
		imd := &s.instancedMeshes[i]
		base := s.firstChildren[imd.ownerNode].index
		gid := imd.geometry.ref.ID()
		for j := 0; j < imd.instanceCount; j++ {
			s.drawables = append(s.drawables, drawable{
				transformID: base + uint32(j),
				geometryID:  gid,
				skeletonID:  noSkeleton,
			})
		}
	}

	s.drawableDirty = false
	return true
}
