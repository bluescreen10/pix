#version 460
#extension GL_EXT_nonuniform_qualifier : require
#extension GL_EXT_scalar_block_layout : require
#extension GL_EXT_samplerless_texture_functions : require
#extension GL_GOOGLE_include_directive : require

// Brings occlusion measured at half resolution up to full, by a bilateral filter: of the
// four occlusion texels around each pixel, the ones whose depth is nearest the pixel's
// own count most, so the occlusion of a surface does not bleed onto the one in front of
// it. The pixel's own depth comes from the scene's depth buffer. Where no surface is,
// nothing is occluded.
#include "occlusion.glsl"

layout(push_constant, scalar) uniform PC {
    vec4 depthUnprojection;
    uint depth;
    uint depthSamples;
    uint occlusion;
    uint chain;
    uint target;
    uint top;
    ivec2 fullSize;
} pc;

// visibilityAt is the visibility measured near pixel, given how far in front of the eye
// it is.
float visibilityAt(ivec2 pixel, float depth) {
    int top = int(pc.top);
    ivec2 size = mipSize(pc.fullSize, top);
    // Texel i stands at i * 2^top + 0.5 (see occlusion.glsl).
    vec2 position = vec2(pixel) / float(1 << top);
    ivec2 base = ivec2(floor(position));
    vec2 fraction = position - vec2(base);

    float sum = 0.0;
    float total = 0.0;
    for (int i = 0; i < 4; i++) {
        ivec2 corner = ivec2(i & 1, i >> 1);
        ivec2 texel = clamp(base + corner, ivec2(0), size - 1);
        vec2 bilinear = mix(1.0 - fraction, fraction, vec2(corner));
        float texelDepth = depthAt(pc.chain, texel, top, top);
        float closeness = 1.0 / (0.001 + abs(texelDepth - depth) / depth);
        float weight = bilinear.x * bilinear.y * closeness;
        sum += texelFetch(gTextures[nonuniformEXT(pc.occlusion)], texel, 0).r * weight;
        total += weight;
    }
    return total > 0.0 ? sum / total : 1.0;
}

layout(local_size_x = 8, local_size_y = 8) in;
void main() {
    ivec2 pixel = ivec2(gl_GlobalInvocationID.xy);
    if (pixel.x >= pc.fullSize.x || pixel.y >= pc.fullSize.y) {
        return;
    }
    // Only compared with the occlusion texels' depths, to weigh them, so a multisampled
    // depth buffer's sample zero, near the pixel's centre, is near enough.
    float depth = pc.depthSamples > 1u
        ? texelFetch(gTexturesMS[nonuniformEXT(pc.depth)], pixel, 0).r
        : texelFetch(gTextures[nonuniformEXT(pc.depth)], pixel, 0).r;
    float viewDepth = linearViewDepth(depth, pc.depthUnprojection);
    float visibility = viewDepth > 0.0 ? visibilityAt(pixel, viewDepth) : 1.0;
    imageStore(gImages[nonuniformEXT(pc.target)], pixel, vec4(visibility));
}
