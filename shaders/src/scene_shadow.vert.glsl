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

// Pushed inline rather than behind a device address: it fits in push constants on
// every backend, so the shader reads its parameters directly instead of chasing a
// pointer to reach them. Fields that are themselves addresses stay addresses — those
// point at unbounded arrays, so that indirection is inherent.
layout(push_constant, scalar) uniform PC {
    mat4 viewProj;
    PosBuf pos;
    DescBuf descs;
    ModelBuf models;
    DrawableBuf drawables;
    VisibleBuf visible;
} pc;

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

    gl_Position = pc.viewProj * (m * vec4(p, 1.0));
}
