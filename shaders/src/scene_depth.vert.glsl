#version 460
#extension GL_EXT_buffer_reference : require
#extension GL_EXT_buffer_reference2 : require
#extension GL_EXT_scalar_block_layout : require
#extension GL_EXT_shader_explicit_arithmetic_types_int64 : require

// The depth-only passes' vertex stage: the shadow maps and the depth prepass. Same
// compaction model as scene_draw.vert (gl_InstanceIndex indexes the view's compacted
// visible buffer, the drawable's geometryID selects a descriptor whose positionBase
// locates the vertex), but stripped to clip position. viewProj is the light's camera, or
// the view's for the prepass.
//
// Built twice. Plain, it reads positions alone: the pass writes depth and nothing else,
// and has no fragment stage. With these defined, it also hands the fragment stage what
// a masked material is cut out by (see scene_depth_masked.frag and materials.Masked):
//
//   USE_UV        the vertex's texture coordinate, from the attribute buffer.
//   USE_MATERIAL  the drawable's material slot, and its pool's mask table.
//   USE_MASK      both: everything the masked fragment stage reads.
//
// The push constants keep their fields in the same order either way, so a define only
// adds fields: renderer.go's roots match each build.

struct Drawable {
    vec4 bounds;
    uint transformID;
    uint geometryID;
    uint materialID;
    uint batchID;
    uint flags;
    uint lodID;
    uint lodLevel;
};
struct GeoDesc {
    uint positionBase;
    uint attributeBase;
    uint indexBase;
    uint indexCount;
    uint flags;
    uint pad;
};

layout(buffer_reference, scalar) readonly buffer PosBuf { float v[]; };
layout(buffer_reference, scalar) readonly buffer AttrBuf { uint v[]; };
layout(buffer_reference, scalar) readonly buffer DescBuf { GeoDesc v[]; };
layout(buffer_reference, scalar) readonly buffer ModelBuf { mat4 v[]; };
layout(buffer_reference, scalar) readonly buffer DrawableBuf { Drawable v[]; };
layout(buffer_reference, scalar) readonly buffer VisibleBuf { uint v[]; };

// Pushed inline rather than behind a device address: it fits in push constants on
// every backend, so the shader reads its parameters directly instead of chasing a
// pointer to reach them. Fields that are themselves addresses stay addresses — those
// point at unbounded arrays, so that indirection is inherent.
layout(push_constant, scalar) uniform PC {
    mat4 viewProj;
    PosBuf pos;
#ifdef USE_MASK
    AttrBuf attr;
#endif
    DescBuf descs;
    ModelBuf models;
    DrawableBuf drawables;
    VisibleBuf visible;
#ifdef USE_MASK
    // masks is the batch's material pool's mask table (see alpha_mask.glsl), read by
    // the fragment stage.
    uint64_t masks;
    uint pad0;
    uint pad1;
#endif
} pc;

#ifdef USE_MASK
layout(location = 0) out vec2 vUV;
layout(location = 1) flat out uint vMat;

const uint FLAG_UV = 2u;
#endif

// A depth prepass compares this shader's depth against one written by a DIFFERENT
// shader, so the two have to agree bit for bit. Nothing otherwise guarantees that: the
// same expression compiled into two programs may fuse a multiply-add in one and not the
// other, and a result a fraction of an ULP behind the prepass fails a GreaterEqual test
// and drops the fragment. That shows up as geometry flickering or missing across the
// whole frame rather than anywhere in particular.
//
// invariant is the guarantee, and it has to be on BOTH shaders to mean anything.
invariant gl_Position;

void main() {
    // firstInstance = the batch's region base, so gl_InstanceIndex already indexes
    // the compacted visible buffer directly (see scene_draw.vert).
    uint di = pc.visible.v[uint(gl_InstanceIndex)];
    Drawable d = pc.drawables.v[di];
    GeoDesc g = pc.descs.v[d.geometryID];
    mat4 m = pc.models.v[d.transformID];

    uint vi = uint(gl_VertexIndex);
    uint pb = g.positionBase + vi * 3u;
    vec3 p = vec3(pc.pos.v[pb], pc.pos.v[pb + 1u], pc.pos.v[pb + 2u]);

#ifdef USE_MASK
    vUV = vec2(0.0);
    if ((g.flags & FLAG_UV) != 0u) {
        uint ab = g.attributeBase * 4u + vi * 4u;
        vUV = vec2(uintBitsToFloat(pc.attr.v[ab + 2u]), uintBitsToFloat(pc.attr.v[ab + 3u]));
    }
    vMat = d.materialID;
#endif

    gl_Position = pc.viewProj * (m * vec4(p, 1.0));
}
