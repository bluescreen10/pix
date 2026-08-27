#version 460
#extension GL_EXT_buffer_reference : require
#extension GL_EXT_buffer_reference2 : require
#extension GL_EXT_scalar_block_layout : require
#extension GL_EXT_shader_explicit_arithmetic_types_int64 : require

// Vertex-pulling draw for the GPU-driven path. gl_InstanceIndex indexes this
// batch's region of the visible buffer (regionBase + local instance) to reach the
// drawable; the drawable's geometryID selects a descriptor, whose bases locate the
// vertex in the shared position/attribute streams. materialID is forwarded (flat)
// to the fragment stage, which samples the bindless heap.
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
layout(buffer_reference, scalar) readonly buffer AttrBuf { uint v[]; };
layout(buffer_reference, scalar) readonly buffer DescBuf { GeoDesc v[]; };
layout(buffer_reference, scalar) readonly buffer ModelBuf { mat4 v[]; };
layout(buffer_reference, scalar) readonly buffer DrawableBuf { Drawable v[]; };
layout(buffer_reference, scalar) readonly buffer VisibleBuf { uint v[]; };

layout(buffer_reference, scalar) readonly buffer DrawRoot {
    mat4 viewProj;
    PosBuf pos;
    AttrBuf attr;
    DescBuf descs;
    ModelBuf models;
    DrawableBuf drawables;
    VisibleBuf visible;
    uint64_t materials;
    uint64_t lights;
    vec4 eye;
};
layout(push_constant) uniform PC { DrawRoot root; } pc;

layout(location = 0) out vec3 vColor;
layout(location = 1) out vec2 vUV;
layout(location = 2) flat out uint vMat;
layout(location = 3) out vec3 vWorldPos;
layout(location = 4) out vec3 vNormal;

const uint FLAG_NORMAL = 1u;
const uint FLAG_UV = 2u;
const uint FLAG_COLOR = 4u;

void main() {
    // Each indirect command sets firstInstance = its region base, so gl_InstanceIndex
    // already includes it and indexes the compacted visible buffer directly.
    uint di = pc.root.visible.v[uint(gl_InstanceIndex)];
    Drawable d = pc.root.drawables.v[di];
    GeoDesc g = pc.root.descs.v[d.geometryID];
    mat4 m = pc.root.models.v[d.transformID];
    vMat = d.materialID;

    uint vi = uint(gl_VertexIndex);
    uint pb = g.positionBase + vi * 3u;
    vec3 p = vec3(pc.root.pos.v[pb], pc.root.pos.v[pb + 1u], pc.root.pos.v[pb + 2u]);

    uint ab = g.attributeBase * 4u + vi * 4u;
    vColor = vec3(1.0);
    if ((g.flags & FLAG_COLOR) != 0u) {
        uint c = pc.root.attr.v[ab + 1u];
        vColor = vec3(float(c & 0xFFu), float((c >> 8) & 0xFFu), float((c >> 16) & 0xFFu)) / 255.0;
    }
    vUV = vec2(0.0);
    if ((g.flags & FLAG_UV) != 0u) {
        vUV = vec2(uintBitsToFloat(pc.root.attr.v[ab + 2u]), uintBitsToFloat(pc.root.attr.v[ab + 3u]));
    }

    // World-space normal (decode RGB10A2 [-1,1]; rotate by the model's 3x3).
    vec3 nrm = vec3(0.0, 0.0, 1.0);
    if ((g.flags & FLAG_NORMAL) != 0u) {
        uint nw = pc.root.attr.v[ab];
        nrm = vec3(float(nw & 0x3FFu), float((nw >> 10) & 0x3FFu), float((nw >> 20) & 0x3FFu)) / 1023.0 * 2.0 - 1.0;
    }
    vNormal = mat3(m) * nrm;

    vec4 wp = m * vec4(p, 1.0);
    vWorldPos = wp.xyz;
    gl_Position = pc.root.viewProj * wp;
}
