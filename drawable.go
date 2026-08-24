package pix

// noSkeleton is the skeletonID sentinel for drawables that are not skinned.
const noSkeleton uint32 = 0xFFFFFFFF

// Drawable flag bits. castsShadow is structural (stamped from the owner node when
// the drawable is built) so the GPU cull can filter shadow casters per light.
const (
	drawableVisible     uint32 = 1 << 0
	drawableCastsShadow uint32 = 1 << 1
)

// drawableFlags derives a drawable's static flags from its owner node.
func drawableFlags(s *Scene, owner uint32) uint32 {
	var f uint32
	if s.flags[owner].CastShadow() {
		f |= drawableCastsShadow
	}
	return f
}

// drawable is one draw-instance and the single unit the render list is built
// from: one per plain and skinned mesh, and one per instance of an instanced
// mesh. It is uploaded verbatim to the drawables storage buffer, so its layout
// mirrors the WESL Drawable struct (std430): bounds is a vec4 and must sit at a
// 16-byte-aligned offset, hence first, and the struct is padded to a 48-byte
// (multiple-of-16) array stride.
//
//   - bounds is the LOCAL (object-space) bounding sphere — structural, so the
//     buffer only re-uploads on add/remove. collectRenderList (and, later, the
//     GPU cull pass) transform it to world space using the model matrix.
//   - instance is this drawable's own index in scene.drawables; it is the draw
//     call's firstInstance, so the vertex shader resolves the transform through
//     drawables[instance].transform_id.
//   - transformID indexes the scene world-transform arrays.
//   - geometryID / materialID / skeletonID index the renderer resource slabs.
//     skeletonID is noSkeleton unless the mesh is skinned.
//   - ownerNode is the node whose flags gate visibility/shadow (the parent for
//     an instanced mesh's instances).
type drawable struct {
	bounds      Sphere
	instance    uint32
	transformID uint32
	geometryID  uint32
	materialID  uint32
	skeletonID  uint32
	ownerNode   uint32
	flags       uint32
	batchID     uint32 // (material, geometry) batch, assigned by the renderer's culler
}

// UpdateDrawables flattens the mesh payload tables into the dense drawables
// list. Called lazily when drawableDirty is set — only on structural changes
// (mesh add/remove, material swap), never per frame. Removal is handled
// implicitly: rebuilding from the current payload tables simply drops the gone
// entries, which is what makes eliminating an instanced mesh's whole run trivial.
func (s *Scene) UpdateDrawables() bool {
	if !s.drawableDirty {
		return false
	}

	s.drawables = s.drawables[:0]

	for i := range s.meshes {
		md := &s.meshes[i]
		s.drawables = append(s.drawables, drawable{
			bounds:      md.boundingSphere,
			instance:    uint32(len(s.drawables)),
			transformID: md.ownerNode,
			geometryID:  md.geometry.ref.ID(),
			materialID:  md.material.ref.ID(),
			skeletonID:  noSkeleton,
			ownerNode:   md.ownerNode,
			flags:       drawableFlags(s, md.ownerNode),
		})
	}

	for i := range s.skinnedMeshes {
		smd := &s.skinnedMeshes[i]
		s.drawables = append(s.drawables, drawable{
			bounds:      smd.boundingSphere,
			instance:    uint32(len(s.drawables)),
			transformID: smd.ownerNode,
			geometryID:  smd.geometry.ref.ID(),
			materialID:  smd.material.ref.ID(),
			skeletonID:  smd.skeleton.ref.ID(),
			ownerNode:   smd.ownerNode,
			flags:       drawableFlags(s, smd.ownerNode),
		})
	}

	// Each instanced mesh expands to one drawable per instance. The instances are
	// consecutive child nodes, so their transforms are base .. base+count-1. They
	// share the owner's flags and the geometry's local bounds but each carries its
	// own transform for per-instance culling.
	for i := range s.instancedMeshes {
		imd := &s.instancedMeshes[i]
		base := s.firstChildren[imd.ownerNode].index
		gid := imd.geometry.ref.ID()
		mid := imd.material.ref.ID()
		flags := drawableFlags(s, imd.ownerNode)
		localBounds := imd.geometry.BoundingSphere()
		for j := 0; j < imd.instanceCount; j++ {
			s.drawables = append(s.drawables, drawable{
				bounds:      localBounds,
				instance:    uint32(len(s.drawables)),
				transformID: base + uint32(j),
				geometryID:  gid,
				materialID:  mid,
				skeletonID:  noSkeleton,
				ownerNode:   imd.ownerNode,
				flags:       flags,
			})
		}
	}

	s.drawableDirty = false
	return true
}
