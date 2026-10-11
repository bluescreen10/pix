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
// behind. In a shadow pass it also writes none where the surface lets light through:
// glass has a surface, which the prepass must keep, but casts no shadow.
//
// Built twice. Plain, it writes nothing else. With USE_MOTION defined, for the prepass
// while motion vectors are on, it also writes how far the surface moved since the
// previous frame, over scene_depth.vert built with USE_MOTION too.
#include <pix/alpha_mask.glsl>
#ifdef USE_MOTION
#include "motion.glsl"
#endif

layout(push_constant, scalar) uniform PC {
    mat4 viewProj;
#ifdef USE_MOTION
    mat4 previousViewProj;
#endif
    uint64_t pos;
    uint64_t attr;
    uint64_t descs;
    uint64_t models;
#ifdef USE_MOTION
    uint64_t previousModels;
    uint64_t previousPositions;
    uint64_t previousPositionBases;
#endif
    uint64_t drawables;
    uint64_t visible;
    uint64_t masks;
    uint isShadowPass;
    uint pad1;
#ifdef USE_MOTION
    vec2 jitter;
#endif
} pc;

layout(location = 0) in vec2 vUV1;
layout(location = 1) flat in uint vMat;
layout(location = 2) in vec2 vUV2;
#ifdef USE_MOTION
layout(location = 3) in vec4 vClip;
layout(location = 4) in vec4 vPreviousClip;

layout(location = 0) out vec2 outMotion;
#endif

void main() {
    Mask mask = MaskBuf(pc.masks).v[vMat];
    if (isCutOut(mask.alpha, maskUV(mask, mask.alpha.uvSet, vUV1, vUV2))) {
        discard;
    }
    if (pc.isShadowPass != 0u && letsLightThrough(mask.transmission, maskUV(mask, mask.transmission.uvSet, vUV1, vUV2))) {
        discard;
    }
#ifdef USE_MOTION
    outMotion = screenMotion(vClip, vPreviousClip);
#endif
}
