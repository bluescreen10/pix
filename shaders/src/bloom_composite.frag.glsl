#version 460
#extension GL_EXT_nonuniform_qualifier : require
#extension GL_EXT_scalar_block_layout : require
#extension GL_GOOGLE_include_directive : require

// Bloom, composite: blend the finished bloom into the scene. A mix rather than an add, so
// the image's total energy is conserved: bloom redistributes light, it does not create it.
#include "postfx.glsl"

layout(push_constant, scalar) uniform PC {
    POSTFX_ROOT
    uint bloom;      // bindless index of the chain's top level
    float intensity; // how much of the image the bloom replaces
    float pad0;
    float pad1;
} pc;

layout(location = 0) out vec4 outColor;

void main() {
    vec3 scene = sampleImage(pc.source, pc.linearSampler, vUV).rgb;
    vec3 glow = sampleImage(pc.bloom, pc.linearSampler, vUV).rgb;
    outColor = vec4(mix(scene, glow, pc.intensity), 1.0);
}
