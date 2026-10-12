#version 460
#extension GL_EXT_nonuniform_qualifier : require
#extension GL_EXT_scalar_block_layout : require
#extension GL_EXT_samplerless_texture_functions : require
#extension GL_GOOGLE_include_directive : require

// Writes the first image of the depth chain from the scene's depth buffer, at the
// resolution occlusion is measured at: each texel how far in front of the eye the
// top-left pixel of the block it covers is (see occlusion.glsl).
#include "occlusion.glsl"

layout(push_constant, scalar) uniform PC {
    vec4 depthUnprojection;
    uint depth;
    uint depthSamples;
    uint target;
    uint top;
    ivec2 fullSize;
} pc;

layout(local_size_x = 8, local_size_y = 8) in;
void main() {
    ivec2 texel = ivec2(gl_GlobalInvocationID.xy);
    int top = int(pc.top);
    ivec2 size = mipSize(pc.fullSize, top);
    if (texel.x >= size.x || texel.y >= size.y) {
        return;
    }
    float viewDepth = linearViewDepth(sceneDepthAt(pc.depth, pc.depthSamples, texel << top), pc.depthUnprojection);
    if (viewDepth <= 0.0) {
        viewDepth = BACKGROUND_DEPTH;
    }
    imageStore(gImages[nonuniformEXT(pc.target)], texel, vec4(min(viewDepth, BACKGROUND_DEPTH)));
}
