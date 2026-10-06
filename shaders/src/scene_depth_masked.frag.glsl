#version 460
#extension GL_EXT_buffer_reference : require
#extension GL_EXT_buffer_reference2 : require
#extension GL_EXT_scalar_block_layout : require
#extension GL_EXT_nonuniform_qualifier : require
#extension GL_EXT_shader_explicit_arithmetic_types_int64 : require
#extension GL_GOOGLE_include_directive : require

// The depth-only passes' fragment stage for masked materials, whatever their type:
// where the material has no surface, by its mask (see alpha_mask.glsl), it writes no
// depth — the shadow map lets light through, and the prepass leaves the pixel to what is
// behind. It writes nothing else.
#include "alpha_mask.glsl"

layout(push_constant, scalar) uniform PC {
    mat4 viewProj;
    uint64_t pos;
    uint64_t attr;
    uint64_t descs;
    uint64_t models;
    uint64_t drawables;
    uint64_t visible;
    uint64_t masks;
    uint pad0;
    uint pad1;
} pc;

layout(location = 0) in vec2 vUV;
layout(location = 1) flat in uint vMat;

void main() {
    if (isCutOut(AlphaMaskBuf(pc.masks).v[vMat], vUV)) {
        discard;
    }
}
