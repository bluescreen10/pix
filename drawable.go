package pix

import (
	"unsafe"

	"github.com/bluescreen10/pix/glm"
	"github.com/bluescreen10/pix/materials"
)

// regionAlign is the per-batch granularity in the visible buffer, in u32 entries.
const regionAlign uint32 = 64

// Drawable flag bits (mirror the GLSL). CastsShadow is structural so a cull pass can
// filter shadow casters; ReceivesShadow lets the lit shaders skip shadow sampling for
// a drawable (both default per-node — see scene flags).
const (
	DrawableCastsShadow    uint32 = 1 << 1
	DrawableReceivesShadow uint32 = 1 << 2
)

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

// drawRoot matches DrawRoot in scene_draw.vert / material_common.glsl (scalar; mat4
// then pointers, then eye). One per pipeline (its material store's buffer address);
// there is no regionBase — each indirect command sets firstInstance instead.
type drawRoot struct {
	viewProj         glm.Mat4f
	pos              uint64
	attr             uint64
	descs            uint64
	models           uint64
	drawables        uint64
	visible          uint64
	materials        uint64
	lights           uint64
	eye              glm.Vec4f
	shadowSampler    uint32 // bindless index of the PCF comparison sampler
	pad0, pad1, pad2 uint32
}

// shadowRoot matches ShadowRoot in scene_shadow.vert (scalar; mat4 then pointers).
// The depth pass is position-only, so this is a stripped drawRoot: no attributes,
// descriptors are still needed to locate the position stream, and visible is the
// shadow view's own compacted buffer. viewProj is the light camera's.
type shadowRoot struct {
	viewProj  glm.Mat4f
	pos       uint64
	descs     uint64
	models    uint64
	drawables uint64
	visible   uint64
}

// skinCmd is one SkinnedMesh's compute-skinning dispatch, built by
// Renderer.skinCommands(): srcDesc is the source geometry's descriptor id (positions,
// attributes, skin records), dstDesc is its persistent compute-output geometry's
// descriptor id, jointBase is its skeleton's offset into the scene's joint buffer,
// and vertexCount sizes the dispatch.
type skinCmd struct {
	srcDesc, dstDesc, jointBase, vertexCount uint32
}

// skinRoot matches SkinRoot in scene_skin.comp (scalar; pointers, then plain
// fields). One per skinned mesh per frame — see Renderer.dispatchSkinning.
type skinRoot struct {
	pos, attr, skin, descs, joints           uint64
	srcDesc, dstDesc, jointBase, vertexCount uint32
}

// lightingRoot matches LightingRoot in pbr_frag.glsl's lighting pass (scalar; mat4,
// then pointers, then plain fields). One per frame — the fullscreen lighting pass
// doesn't batch by geometry. The *Texture fields are bindless heap indices.
type lightingRoot struct {
	invViewProj     glm.Mat4f
	eye             glm.Vec4f
	lights          uint64
	shadowSampler   uint32
	gbufferSampler  uint32
	diffuseTexture  uint32
	normalTexture   uint32
	materialTexture uint32
	emissiveTexture uint32
	depthTexture    uint32
	screen          [2]float32
	pad0            uint32
}

// batch is one indirect command: a run of drawables sharing a (pipeline, geometry)
// pair. materials.Material is NOT a batch key — per-drawable materialID selects the record in
// the pipeline's store, so all materials of a type sharing a geometry draw together.
// Its region in the visible buffer is [regionBase, regionBase+regionCap).
type batch struct {
	pipeline   uint32
	geometryID uint32
	regionBase uint32
	regionCap  uint32
}

// pipelineRun is a contiguous span of batches sharing a pipeline, drawn with one
// multi-draw-indirect call. mat is any material of the run (they share a store), used
// to read the store's current record-buffer address at draw time.
type pipelineRun struct {
	pipeline   uint32
	firstBatch uint32
	count      uint32
	mat        materials.Material
}

var (
	drawableSize     = uint32(unsafe.Sizeof(gpuDrawable{}))
	indirectSize     = uint32(unsafe.Sizeof(indirectCmd{}))
	drawRootSize     = uint64(unsafe.Sizeof(drawRoot{}))
	cullRootSize     = uint64(unsafe.Sizeof(cullRoot{}))
	shadowRootSize   = uint64(unsafe.Sizeof(shadowRoot{}))
	lightingRootSize = uint64(unsafe.Sizeof(lightingRoot{}))
	skinRootSize     = uint64(unsafe.Sizeof(skinRoot{}))
)
