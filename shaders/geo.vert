#version 460
#extension GL_EXT_buffer_reference : require
#extension GL_EXT_buffer_reference2 : require
#extension GL_EXT_scalar_block_layout : require
#extension GL_EXT_shader_explicit_arithmetic_types_int64 : require

// Vertex-pulling from the bindless GeometrySystem streams. Positions are a flat
// float stream (base in float units); the interleaved attribute stream packs one
// 16-byte record per vertex (word0 normal RGB10A2, word1 color RGBA8, words2-3 uv).
layout(buffer_reference, scalar) readonly buffer PosBuf { float v[]; };
layout(buffer_reference, scalar) readonly buffer AttrBuf { uint v[]; };

struct GeoDesc {
    uint positionBase;  // float units (byteOffset / 4)
    uint attributeBase; // 16-byte-record units (byteOffset / 16)
    uint indexBase;
    uint indexCount;
    uint flags;
    uint pad;
};
layout(buffer_reference, scalar) readonly buffer DescBuf { GeoDesc v[]; };

layout(buffer_reference, scalar) readonly buffer Root {
    mat4 mvp;
    PosBuf pos;
    AttrBuf attr;
    DescBuf descs;
    uint geoID;
};
layout(push_constant) uniform PC { Root root; } pc;

layout(location = 0) out vec3 vColor;

const uint FLAG_NORMAL = 1u;
const uint FLAG_UV = 2u;
const uint FLAG_COLOR = 4u;

void main() {
    GeoDesc d = pc.root.descs.v[pc.root.geoID];
    uint vi = uint(gl_VertexIndex);

    uint pb = d.positionBase + vi * 3u;
    vec3 p = vec3(pc.root.pos.v[pb], pc.root.pos.v[pb + 1u], pc.root.pos.v[pb + 2u]);

    vColor = vec3(1.0);
    if ((d.flags & FLAG_COLOR) != 0u) {
        uint c = pc.root.attr.v[d.attributeBase * 4u + vi * 4u + 1u];
        vColor = vec3(float(c & 0xFFu), float((c >> 8) & 0xFFu), float((c >> 16) & 0xFFu)) / 255.0;
    } else if ((d.flags & FLAG_NORMAL) != 0u) {
        uint nrm = pc.root.attr.v[d.attributeBase * 4u + vi * 4u];
        vec3 n = vec3(float(nrm & 0x3FFu), float((nrm >> 10) & 0x3FFu), float((nrm >> 20) & 0x3FFu)) / 1023.0;
        vColor = n; // stored [-1,1]->[0,1]; use directly as a debug color
    }

    gl_Position = pc.root.mvp * vec4(p, 1.0);
}
