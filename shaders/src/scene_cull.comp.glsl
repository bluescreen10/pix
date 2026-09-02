#version 460
#extension GL_EXT_buffer_reference : require
#extension GL_EXT_buffer_reference2 : require
#extension GL_EXT_scalar_block_layout : require
#extension GL_EXT_shader_explicit_arithmetic_types_int64 : require

// GPU-driven frustum cull + batch compaction. One thread per drawable: transform
// its local bounding sphere to world, frustum-test, and on survival bump its
// batch's indirect instanceCount and append its index into the batch's region of
// the shared visible buffer. Mirrors pix's cull.wesl on the bindless gpu.
layout(local_size_x = 64) in;

struct Drawable {
    vec4 bounds;      // xyz local center, w radius
    uint transformID;
    uint geometryID;
    uint materialID;
    uint batchID;
    uint flags;
};
struct IndirectCmd { uint indexCount; uint instanceCount; uint firstIndex; int vertexOffset; uint firstInstance; };

layout(buffer_reference, scalar) readonly buffer DrawableBuf { Drawable v[]; };
layout(buffer_reference, scalar) readonly buffer ModelBuf { mat4 v[]; };
layout(buffer_reference, scalar) buffer IndirectBuf { IndirectCmd v[]; };
layout(buffer_reference, scalar) readonly buffer RegionBuf { uint v[]; };
layout(buffer_reference, scalar) buffer VisibleBuf { uint v[]; };

layout(buffer_reference, scalar) readonly buffer CullRoot {
    DrawableBuf drawables;
    ModelBuf models;
    IndirectBuf indirect;
    RegionBuf regions;
    VisibleBuf visible;
    uint count;
    uint castersOnly;
    vec4 planes[6];
};
layout(push_constant) uniform PC { CullRoot root; } pc;

const uint FLAG_CASTS_SHADOW = 2u;

void main() {
    uint i = gl_GlobalInvocationID.x;
    if (i >= pc.root.count) return;
    Drawable d = pc.root.drawables.v[i];
    if (pc.root.castersOnly != 0u && (d.flags & FLAG_CASTS_SHADOW) == 0u) return;

    mat4 m = pc.root.models.v[d.transformID];
    vec3 center = (m * vec4(d.bounds.xyz, 1.0)).xyz;
    float s = max(length(m[0].xyz), max(length(m[1].xyz), length(m[2].xyz)));
    float radius = d.bounds.w * s;
    for (int p = 0; p < 6; p++) {
        vec4 pl = pc.root.planes[p];
        if (dot(pl.xyz, center) + pl.w < -radius) return;
    }

    uint bid = d.batchID;
    uint slot = atomicAdd(pc.root.indirect.v[bid].instanceCount, 1u);
    pc.root.visible.v[pc.root.regions.v[bid] + slot] = i;
}
