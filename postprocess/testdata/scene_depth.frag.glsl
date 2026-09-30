#version 460
#extension GL_EXT_nonuniform_qualifier : require
#extension GL_EXT_scalar_block_layout : require
#extension GL_GOOGLE_include_directive : require

// scene_depth draws the scene's depth, which the root hands every post-processing pass,
// as a grey level: near is bright, the far plane black.
#include "../../shaders/src/postfx.glsl"

layout(push_constant, scalar) uniform PC {
    POSTFX_ROOT
} pc;

layout(location = 0) out vec4 outColor;

void main() {
    float depth = sampleImage(pc.sceneDepth, pc.linearSampler, vUV).r;
    outColor = vec4(vec3(depth), 1.0);
}
