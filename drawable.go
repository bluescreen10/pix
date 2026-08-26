package pix

import "unsafe"

// regionAlign is the per-batch granularity in the visible buffer, in u32 entries.
const regionAlign uint32 = 64

// Drawable flag bits (mirror the GLSL). castsShadow is structural so a cull pass
// can filter shadow casters.
const DrawableCastsShadow uint32 = 1 << 1

// gpuDrawable is uploaded verbatim to the drawable buffer; matches Drawable in the
// scene shaders (scalar, 40 bytes). bounds is the LOCAL bounding sphere; transformID
// indexes the scene's world-matrix buffer (a node slot); geometryID/materialID index
// the renderer's geometry/material tables.
type gpuDrawable struct {
	bounds      [4]float32
	transformID uint32
	geometryID  uint32
	materialID  uint32
	batchID     uint32
	flags       uint32
}

// indirectCmd is a VkDrawIndexedIndirectCommand (20 bytes).
type indirectCmd struct {
	indexCount    uint32
	instanceCount uint32
	firstIndex    uint32
	vertexOffset  int32
	firstInstance uint32
}

// cullRoot matches CullRoot in scene_cull.comp (scalar; pointers-first).
type cullRoot struct {
	drawables   uint64
	models      uint64
	indirect    uint64
	regions     uint64
	visible     uint64
	count       uint32
	castersOnly uint32
	planes      [6][4]float32
}

// drawRoot matches DrawRoot in scene_draw.vert / scene_lit.frag (scalar; mat4 then
// pointers grouped so nothing needs interior padding, then regionBase + eye).
type drawRoot struct {
	viewProj   [16]float32
	pos        uint64
	attr       uint64
	descs      uint64
	models     uint64
	drawables  uint64
	visible    uint64
	materials  uint64
	lights     uint64
	regionBase uint32
	eye        [4]float32
}

// batch is a run of drawables sharing a (geometry, material) pair — one indirect
// draw. Its region in the visible buffer is [regionBase, regionBase+regionCap).
type batch struct {
	geometryID uint32
	materialID uint32
	regionBase uint32
	regionCap  uint32
}

var (
	drawableSize = uint32(unsafe.Sizeof(gpuDrawable{}))
	indirectSize = uint32(unsafe.Sizeof(indirectCmd{}))
	drawRootSize = uint64(unsafe.Sizeof(drawRoot{}))
)
