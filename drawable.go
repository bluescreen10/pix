package pix

import (
	"unsafe"

	"github.com/bluescreen10/pix/glm"
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
// indexes drawList.lods, and lodLevel is this record's own 0-based level within that
// entry — see the LOD spec: a mesh with N LOD levels expands to N
// gpuDrawable records sharing one lodID (and, for Mesh/SkinnedMesh, one transformID),
// and scene_cull.comp lets through at most one of them per frame.
type gpuDrawable struct {
	bounds      glm.Sphere
	transformID uint32
	geometryID  uint32
	materialID  uint32
	batchID     uint32
	flags       uint32
	lodID       uint32
	lodLevel    uint32
}

// gpuLOD is one LOD group's shared config (mirrors LodEntry in scene_cull.comp):
// up to 4 levels, boundaries[i] is the far edge (world units, distance from camera) of
// level i for i < levelCount-1 — the last level has no upper bound. hysteresis widens
// whichever level was selected last frame (see drawList.prevLevelBuf and
// scene_cull.comp's selectLevel) to resist flip-flopping right at a boundary.
// drawList.lods[0] is reserved/unused so a drawable's lodID of 0 unambiguously means
// "not LOD-tagged".
// lodNoneSentinel marks a prevLevelBuf slot as "no level selected yet" — out of range
// for any real levelCount (max maxLODLevels), so selectLevel's hysteresis widening
// never accidentally matches it.
const lodNoneSentinel uint32 = 0xFFFFFFFF

type gpuLOD struct {
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
	visible     uint64
	lods        uint64
	prevLevel   uint64
	eye         glm.Vec4f
	count       uint32
	castersOnly uint32
	planes      [6]glm.Vec4f
	// Pads 168 bytes up to the 176 MSL rounds the shader's struct to (see the note on
	// the *Root types above). It was a multiple of 16 until the region table went.
	pad0, pad1 uint32
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
	time float32
	// directShareInAlpha has a material write, where its alpha goes, the share of its
	// colour that ambient occlusion leaves alone (see outputAlpha in
	// material_common.glsl). Set in the opaque pass while ambient occlusion is on.
	directShareInAlpha uint32
	pad                uint32
	// masks is the material pool's mask table (see materials.Masked) while drawing a
	// span of masked materials, and 0 otherwise: what discardCutOut reads.
	masks uint64
	// sceneCopy is the heap index of the scene copy (see sceneCopy) in a transparent pass
	// that has one, and noSceneCopy otherwise; sceneCopySampler is what it is read with.
	sceneCopy        uint32
	sceneCopySampler uint32
}

// positionRoot is the root of every position-only pass: the shadow depth pass, the
// depth prepass, and the object/triangle debug views. It matches the push constant in
// scene_depth.vert and scene_debug_id.vert, which declare the same block.
//
// A stripped drawRoot: no attributes and no material, but descriptors are still needed
// to locate the position stream. viewProj is whichever camera the pass renders from, and
// visible is whichever compacted buffer its cull filled.
type positionRoot struct {
	viewProj  glm.Mat4f
	pos       uint64
	descs     uint64
	models    uint64
	drawables uint64
	visible   uint64
	// The mat4 gives the struct 16-byte alignment, and MSL rounds a struct's size up to
	// its alignment: the shader's argument is 112 bytes, not the 104 Go packs to. Without
	// these the Metal backend rejects the draw — "data is 104 bytes but the shader's root
	// argument is 112" — rather than letting the shader read past a short buffer.
	pad0, pad1 uint32
}

// maskedDepthRoot is the root of the depth-only passes for a span of masked materials:
// the position root plus what scene_depth_masked.vert and .frag cut the surface out by.
type maskedDepthRoot struct {
	viewProj  glm.Mat4f
	pos       uint64
	attr      uint64
	descs     uint64
	models    uint64
	drawables uint64
	visible   uint64
	masks     uint64
	// isShadowPass leaves out of the depth the texels that let light through (see
	// materials.Transmissive): set for a shadow map, whose glass casts no shadow, and
	// not for the prepass, which glass must still occlude.
	isShadowPass uint32
	_            uint32
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
// fields). One per skinned mesh per frame — see Renderer.encodeSkinning.
type skinRoot struct {
	pos, attr, skin, descs, joints           uint64
	srcDesc, dstDesc, jointBase, vertexCount uint32
}

var (
	drawableSize = uint32(unsafe.Sizeof(gpuDrawable{}))
	indirectSize = uint32(unsafe.Sizeof(indirectCmd{}))
	lodEntrySize = uint32(unsafe.Sizeof(gpuLOD{}))
)
