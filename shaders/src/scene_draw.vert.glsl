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
    AttrBuf attr;
    DescBuf descs;
    ModelBuf models;
    DrawableBuf drawables;
    VisibleBuf visible;
    uint64_t materials;
    uint64_t lights;
    vec4 eye;
    uint shadowSampler;
    // time is elapsed seconds since the scene's clock started (Scene.clockStart),
    // passed to every vertex/fragment shader pair unconditionally — read it or not.
    float time;
    uint spad0;
    uint spad1;
    uint64_t masks;
    uint spad2;
    uint spad3;
} pc;

layout(location = 0) out vec3 vColor;
layout(location = 1) out vec2 vUV;
layout(location = 2) flat out uint vMat;
layout(location = 3) out vec3 vWorldPos;
layout(location = 4) out vec3 vNormal;
layout(location = 5) flat out uint vFlags;

const uint FLAG_NORMAL = 1u;
const uint FLAG_UV = 2u;
const uint FLAG_COLOR = 4u;

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
    // Each indirect command sets firstInstance = its region base, so gl_InstanceIndex
    // already includes it and indexes the compacted visible buffer directly.
    uint di = pc.visible.v[uint(gl_InstanceIndex)];
    Drawable d = pc.drawables.v[di];
    GeoDesc g = pc.descs.v[d.geometryID];
    mat4 m = pc.models.v[d.transformID];
    vMat = d.materialID;
    vFlags = d.flags;

    uint vi = uint(gl_VertexIndex);
    uint pb = g.positionBase + vi * 3u;
    vec3 p = vec3(pc.pos.v[pb], pc.pos.v[pb + 1u], pc.pos.v[pb + 2u]);

    uint ab = g.attributeBase * 4u + vi * 4u;
    vColor = vec3(1.0);
    if ((g.flags & FLAG_COLOR) != 0u) {
        uint c = pc.attr.v[ab + 1u];
        vColor = vec3(float(c & 0xFFu), float((c >> 8) & 0xFFu), float((c >> 16) & 0xFFu)) / 255.0;
    }
    vUV = vec2(0.0);
    if ((g.flags & FLAG_UV) != 0u) {
        vUV = vec2(uintBitsToFloat(pc.attr.v[ab + 2u]), uintBitsToFloat(pc.attr.v[ab + 3u]));
    }

    // World-space normal: decode glm.Unorm10x3 and remap back to [-1,1] (the
    // inverse of the pack in geometry.go), then rotate by the model's 3x3.
    vec3 nrm = vec3(0.0, 0.0, 1.0);
    if ((g.flags & FLAG_NORMAL) != 0u) {
        uint nw = pc.attr.v[ab];
        nrm = vec3(float(nw & 0x3FFu), float((nw >> 10) & 0x3FFu), float((nw >> 20) & 0x3FFu)) / 1023.0 * 2.0 - 1.0;
    }
    vNormal = mat3(m) * nrm;

    vec4 wp = m * vec4(p, 1.0);
    vWorldPos = wp.xyz;
    gl_Position = pc.viewProj * wp;
}
