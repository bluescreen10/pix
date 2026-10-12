#version 460
#extension GL_EXT_nonuniform_qualifier : require
#extension GL_EXT_scalar_block_layout : require
#extension GL_EXT_samplerless_texture_functions : require
#extension GL_GOOGLE_include_directive : require

// Splits the scene's depth into the four interleaved images ASSAO gathers from, as view
// depths: each texel the depth of the pixel it stands for (see interleavedPixel), 0
// where nothing was drawn.
#include "assao.glsl"

layout(push_constant, scalar) uniform PC {
    vec4 depthUnprojection;
    uint depth;
    uint depthSamples;
    int stride;
    uint targets[4];
    ivec2 interleavedSize;
    ivec2 fullSize;
} pc;

layout(local_size_x = 8, local_size_y = 8) in;
void main() {
    ivec2 texel = ivec2(gl_GlobalInvocationID.xy);
    if (texel.x >= pc.interleavedSize.x || texel.y >= pc.interleavedSize.y) {
        return;
    }
    for (uint interleaved = 0u; interleaved < 4u; interleaved++) {
        ivec2 pixel = min(ivec2(interleavedPixel(texel, interleaved, pc.stride)), pc.fullSize - 1);
        float viewDepth = linearViewDepth(sceneDepthAt(pc.depth, pc.depthSamples, pixel), pc.depthUnprojection);
        imageStore(gImages[nonuniformEXT(pc.targets[interleaved])], texel, vec4(viewDepth));
    }
}
