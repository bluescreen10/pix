#version 460
#extension GL_EXT_nonuniform_qualifier : require
#extension GL_EXT_scalar_block_layout : require
#extension GL_GOOGLE_include_directive : require

// Bloom, upsample: a 3x3 tent filter over a smaller level, blended additively into the
// next larger one. Walking the chain back up this way sums every level's blur into the
// top one, which is what gives bloom its wide, soft falloff.
#include "postfx.glsl"

layout(push_constant, scalar) uniform PC {
    POSTFX_ROOT
    float radius; // tent radius, in texels of the source level
    float pad0;
    float pad1;
    float pad2;
} pc;

layout(location = 0) out vec4 outColor;

vec3 tap(float x, float y) {
    return sampleImage(pc.source, pc.linearSampler, vUV + vec2(x, y) * pc.radius * pc.texelSize).rgb;
}

void main() {
    vec3 up = tap(0.0, 0.0) * 4.0;
    up += (tap(0.0, 1.0) + tap(-1.0, 0.0) + tap(1.0, 0.0) + tap(0.0, -1.0)) * 2.0;
    up += tap(-1.0, 1.0) + tap(1.0, 1.0) + tap(-1.0, -1.0) + tap(1.0, -1.0);
    outColor = vec4(up / 16.0, 1.0);
}
