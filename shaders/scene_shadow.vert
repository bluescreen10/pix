#version 460
#extension GL_EXT_buffer_reference : require
#extension GL_EXT_buffer_reference2 : require
#extension GL_EXT_scalar_block_layout : require
#extension GL_EXT_shader_explicit_arithmetic_types_int64 : require

// Position-only vertex-pull for the shadow depth pass. Same compaction model as
// scene_draw.vert (gl_InstanceIndex indexes the view's compacted visible buffer,
// the drawable's geometryID selects a descriptor whose positionBase locates the
// vertex), but stripped to just clip position — the pass writes depth only, so no
// attributes, normals, UVs or material are read. viewProj is the light's camera.
struct Drawable {
    vec4 bounds;
    uint transformID;
    uint geometryID;
    uint materialID;
    uint batchID;
    uint flags;
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

layout(buffer_reference, scalar) readonly buffer ShadowRoot {
    mat4 viewProj;
    PosBuf pos;
    DescBuf descs;
    ModelBuf models;
    DrawableBuf drawables;
    VisibleBuf visible;
};
layout(push_constant) uniform PC { ShadowRoot root; } pc;

void main() {
    // firstInstance = the batch's region base, so gl_InstanceIndex already indexes
    // the compacted visible buffer directly (see scene_draw.vert).
    uint di = pc.root.visible.v[uint(gl_InstanceIndex)];
    Drawable d = pc.root.drawables.v[di];
    GeoDesc g = pc.root.descs.v[d.geometryID];
    mat4 m = pc.root.models.v[d.transformID];

    uint vi = uint(gl_VertexIndex);
    uint pb = g.positionBase + vi * 3u;
    vec3 p = vec3(pc.root.pos.v[pb], pc.root.pos.v[pb + 1u], pc.root.pos.v[pb + 2u]);

    gl_Position = pc.root.viewProj * (m * vec4(p, 1.0));
}
