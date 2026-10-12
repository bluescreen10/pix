package materials

import (
	"unsafe"

	"github.com/bluescreen10/pix/glm"
)

// UVSet names one of a mesh's two sets of texture coordinates: UV1, its
// geometries.AttributeUV, or UV2, its geometries.AttributeUV2 — what glTF calls
// TEXCOORD_0 and TEXCOORD_1. Each map of a material reads one, UV1 unless told
// otherwise: a lightmap or a baked occlusion map often reads UV2, laid out without the
// overlaps and repeats UV1 has for tiling a texture.
type UVSet uint8

const (
	UV1 UVSet = iota
	UV2
)

// UVTransform moves a set of a mesh's texture coordinates before a material samples its
// maps with them: scaled by Scale, then shifted by Offset — KHR_texture_transform
// without its rotation. Scale (2, 2) tiles a texture twice across the surface; Offset
// (0.5, 0) slides it half a texture along u.
//
// A material has one transform for each UVSet, which every map reading that set
// shares, so its normal map stays in step with its colour map, and its alpha mask is cut
// where its colour map shows. A pipeline whose materials all leave their coordinates as
// they are skips the transforms (see Pool.IsUVTransformedAt).
type UVTransform struct {
	Scale  glm.Vec2f
	Offset glm.Vec2f
}

// NoUVTransform leaves texture coordinates as they are. It is what every built-in
// material starts with.
var NoUVTransform = UVTransform{Scale: glm.Vec2f{1, 1}}

// UVTransformer is implemented by materials whose maps are sampled through a
// UVTransform for each UVSet. A material that does not implement it is drawn with the
// mesh's coordinates as they are. One that does reports a change of either transform by
// calling its pool's MarkDirty, as for any change to its record.
type UVTransformer interface {
	UVTransform(set UVSet) UVTransform
}

// isUVTransformed reports whether inst moves either set of the mesh's texture
// coordinates: whether it implements UVTransformer, with a transform other than
// NoUVTransform.
func isUVTransformed(inst Instance) bool {
	transformer, ok := inst.(UVTransformer)
	if !ok {
		return false
	}
	return transformer.UVTransform(UV1) != NoUVTransform || transformer.UVTransform(UV2) != NoUVTransform
}

// ToBytes is the transform as the shaders read it: UVTransform in
// shaders/lib/alpha_mask.glsl, 16 bytes.
func (t UVTransform) ToBytes() []byte {
	return unsafe.Slice((*byte)(unsafe.Pointer(&t)), unsafe.Sizeof(t))
}

// uvSetOf is the UVSet the map flagged mapFlag reads, by uv2Maps: the flags (MatColorMap
// and the others) of the maps that read UV2.
func uvSetOf(uv2Maps, mapFlag uint32) UVSet {
	if uv2Maps&mapFlag != 0 {
		return UV2
	}
	return UV1
}

// withUVSet is uv2Maps with the map flagged mapFlag reading set.
func withUVSet(uv2Maps, mapFlag uint32, set UVSet) uint32 {
	if set == UV2 {
		return uv2Maps | mapFlag
	}
	return uv2Maps &^ mapFlag
}
