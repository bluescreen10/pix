#version 460
#extension GL_EXT_buffer_reference : require
#extension GL_EXT_buffer_reference2 : require
#extension GL_EXT_scalar_block_layout : require
#extension GL_EXT_shader_explicit_arithmetic_types_int64 : require

// Position-only vertex-pull for the object/triangle-id debug view (see
// recordDebugIDView) — a stripped scene_draw.vert, same shape as scene_shadow.vert:
// no attributes, normals, UVs or material are read, just clip position plus the
// drawable index forwarded for the object-id mode (see scene_debug_id.frag.glsl;
// triangle-id mode instead reads gl_PrimitiveID there directly, a free fragment-stage
// builtin that needs no vertex forwarding).
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
layout(buffer_reference, scalar) readonly buffer DescBuf { GeoDesc v[]; };
layout(buffer_reference, scalar) readonly buffer ModelBuf { mat4 v[]; };
layout(buffer_reference, scalar) readonly buffer DrawableBuf { Drawable v[]; };
layout(buffer_reference, scalar) readonly buffer VisibleBuf { uint v[]; };

layout(push_constant, scalar) uniform PC {
    mat4 viewProj;
    PosBuf pos;
    DescBuf descs;
    ModelBuf models;
    DrawableBuf drawables;
    VisibleBuf visible;
} pc;

layout(location = 0) flat out uint vObjectID;

void main() {
    // firstInstance = the batch's region base, so gl_InstanceIndex already indexes
    // the compacted visible buffer directly (see scene_draw.vert).
    uint di = pc.visible.v[uint(gl_InstanceIndex)];
    Drawable d = pc.drawables.v[di];
    GeoDesc g = pc.descs.v[d.geometryID];
    mat4 m = pc.models.v[d.transformID];

    vObjectID = di;

    uint vi = uint(gl_VertexIndex);
    uint pb = g.positionBase + vi * 3u;
    vec3 p = vec3(pc.pos.v[pb], pc.pos.v[pb + 1u], pc.pos.v[pb + 2u]);

    gl_Position = pc.viewProj * (m * vec4(p, 1.0));
}
