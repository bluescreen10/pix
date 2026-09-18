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
// scene shaders (scalar, 44 bytes). bounds is the LOCAL bounding sphere; transformID
// indexes the scene's world-matrix buffer (a node slot, or an InstancedMesh instance —
// see FramePacket.InstanceTransforms); geometryID/materialID index the renderer's
// geometry/material tables. lodID is 0 for an ordinary (non-LOD) drawable; otherwise it
// indexes drawList.lodEntries, and lodLevel is this record's own 0-based level within that
// entry — see the LOD spec: a mesh with N LOD levels expands to N
// gpuDrawable records sharing one lodID (and, for Mesh/SkinnedMesh, one transformID),
// and scene_cull.comp lets through at most one of them per frame.
type gpuDrawable struct {
	bounds      [4]float32
	transformID uint32
	geometryID  uint32
	materialID  uint32
	batchID     uint32
	flags       uint32
	lodID       uint32
	lodLevel    uint32
}

// gpuLODEntry is one LOD group's shared config (mirrors LodEntry in scene_cull.comp):
// up to 4 levels, boundaries[i] is the far edge (world units, distance from camera) of
// level i for i < levelCount-1 — the last level has no upper bound. hysteresis widens
// whichever level was selected last frame (see drawList.prevLevelBuf and
// scene_cull.comp's selectLevel) to resist flip-flopping right at a boundary.
// drawList.lodEntries[0] is reserved/unused so a drawable's lodID of 0 unambiguously means
// "not LOD-tagged".
// lodNoneSentinel marks a prevLevelBuf slot as "no level selected yet" — out of range
// for any real levelCount (max maxLODLevels), so selectLevel's hysteresis widening
// never accidentally matches it.
const lodNoneSentinel uint32 = 0xFFFFFFFF

type gpuLODEntry struct {
	boundaries [3]float32
	hysteresis float32
	levelCount uint32
	pad0, pad1 uint32
}

// indirectCmd is a VkDrawIndexedIndirectCommand (20 bytes).
type indirectCmd struct {
	indexCount    uint32
	instanceCount uint32
	firstIndex    uint32
	vertexOffset  int32
	firstInstance uint32
}

// The *Root types below are the push-constant roots handed to a draw or dispatch.
// Each must be at least as large as the shader's corresponding struct, and MSL rounds
// a struct's size up to its alignment — 16 bytes if any member is a vec4 or mat4, else
// 8 — where Go stops at 8. Pad every root to a multiple of 16 rather than working out
// which members force the alignment; the spare bytes are never read. The Metal backend
// checks this against pipeline reflection and panics on a short root, so a mistake here
// surfaces as an error rather than as a shader reading past the data.

// cullRoot matches CullRoot in scene_cull.comp (scalar; pointers-first). lods and
// prevLevel are always valid addresses (allocated lazily, empty-but-valid when the
// scene has no LOD drawables — see drawList.ensureBuffers) so the shader never needs a
// null check; eye is the camera's world position, used only for LOD distance tests.
type cullRoot struct {
	drawables   uint64
	models      uint64
	indirect    uint64
	regions     uint64
	visible     uint64
	lods        uint64
	prevLevel   uint64
	eye         glm.Vec4f
	count       uint32
	castersOnly uint32
	planes      [6]glm.Vec4f
}

// drawRoot matches DrawRoot in scene_draw.vert / material_common.glsl (scalar; mat4
// then pointers, then eye). One per pipeline (its material store's buffer address);
// there is no regionBase — each indirect command sets firstInstance instead.
type drawRoot struct {
	viewProj      glm.Mat4f
	pos           uint64
	attr          uint64
	descs         uint64
	models        uint64
	drawables     uint64
	visible       uint64
	materials     uint64
	lights        uint64
	eye           glm.Vec4f
	shadowSampler uint32 // bindless index of the PCF comparison sampler
	// time is elapsed seconds since the producer's clock started (FramePacket.Time) —
	// passed to every vertex/fragment shader pair unconditionally, built-in or a
	// custom material's; nothing requires reading it (see material_common.glsl and
	// scene_draw.vert.glsl, which both declare it but only some shaders use it).
	time       float32
	pad0, pad1 uint32
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
	// The mat4 gives the struct 16-byte alignment, and MSL rounds a struct's size up
	// to its alignment: the shader's argument is 112 bytes, not the 104 Go packs to.
	// Without this the Metal backend hands setBytes a short buffer and the shader
	// reads eight bytes past it (drawRoot pads for the same reason).
	pad0, pad1 uint32
}

// debugIDRoot matches DebugIDRoot in scene_debug_id.vert/.frag (scalar; mat4 then
// pointers). Another position-only stripped drawRoot, same shape as shadowRoot — see
// its doc comment for why the trailing padding matters. mode selects which id the
// fragment shader colors by: 0 = per-object (the drawable index), 1 = per-triangle
// (gl_PrimitiveID, a free fragment-stage builtin — no vertex forwarding needed for
// it). See DebugObjectID/DebugTriangleID and recordDebugIDView.
type debugIDRoot struct {
	viewProj  glm.Mat4f
	pos       uint64
	descs     uint64
	models    uint64
	drawables uint64
	visible   uint64
	mode      uint32
	pad0      uint32
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
	// debugView is the G-buffer target the debug pass shows (see DebugView). The
	// lighting pass ignores it; it sits in what was the struct's tail padding, so it
	// costs nothing and both fullscreen passes keep one root layout.
	debugView uint32
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
// multi-draw-indirect call. pool is the material pool every material of the run lives
// in (a pipeline is resolved from a pool, so they cannot differ), read at draw time for
// its current record-buffer address — which moves when the pool grows.
type pipelineRun struct {
	pipeline   uint32
	firstBatch uint32
	count      uint32
	pool       *materials.Pool
}

var (
	drawableSize = uint32(unsafe.Sizeof(gpuDrawable{}))
	indirectSize = uint32(unsafe.Sizeof(indirectCmd{}))
	lodEntrySize = uint32(unsafe.Sizeof(gpuLODEntry{}))
)
