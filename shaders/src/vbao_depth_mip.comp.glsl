#version 460
#extension GL_EXT_nonuniform_qualifier : require
#extension GL_EXT_scalar_block_layout : require
#extension GL_EXT_samplerless_texture_functions : require
#extension GL_GOOGLE_include_directive : require

// Writes one mip of the depth chain from the mip above it: each texel the depth of the
// surface its four children lie on, where they lie on one flat surface, at the centre of
// their positions — so that a sample reading it there finds the surface exactly — and
// otherwise the farthest of them, which leaves no surface standing in front of where
// one is.
//
// XeGTAO averages the children instead, each weighed by how near it lies to the
// farthest; on a floor seen at a slant the nearer children weigh less, the average lies
// below the floor, and samples reading it found the floor occluding itself.
#include "occlusion.glsl"

layout(push_constant, scalar) uniform PC {
    uint chain;
    uint target;
    uint mip;
    uint top;
    ivec2 fullSize;
} pc;

// PLANE_TOLERANCE is how far, as a share of the largest, the sums of the four children's
// reciprocal depths along each diagonal may differ for them to lie on one flat surface.
const float PLANE_TOLERANCE = 0.002;

layout(local_size_x = 8, local_size_y = 8) in;
void main() {
    ivec2 texel = ivec2(gl_GlobalInvocationID.xy);
    int mip = int(pc.mip);
    int top = int(pc.top);
    ivec2 size = mipSize(pc.fullSize, mip);
    if (texel.x >= size.x || texel.y >= size.y) {
        return;
    }
    ivec2 first = texel * 2;
    ivec2 last = mipSize(pc.fullSize, mip - 1) - 1;
    vec4 depths = vec4(
        depthAt(pc.chain, min(first, last), mip - 1, top),
        depthAt(pc.chain, min(first + ivec2(1, 0), last), mip - 1, top),
        depthAt(pc.chain, min(first + ivec2(0, 1), last), mip - 1, top),
        depthAt(pc.chain, min(first + ivec2(1, 1), last), mip - 1, top));
    float farthest = max(max(depths.x, depths.y), max(depths.z, depths.w));

    // On a flat surface it is the reciprocal of depth that changes linearly across the
    // screen: its diagonals sum the same, and its mean is the surface's at the centre.
    float depth = farthest;
    if (farthest < BACKGROUND_DEPTH) {
        vec4 reciprocals = 1.0 / depths;
        float largest = max(max(reciprocals.x, reciprocals.y), max(reciprocals.z, reciprocals.w));
        if (abs(reciprocals.x + reciprocals.w - reciprocals.y - reciprocals.z) <= PLANE_TOLERANCE * largest) {
            depth = 4.0 / dot(reciprocals, vec4(1.0));
        }
    }
    imageStore(gImages[nonuniformEXT(pc.target)], texel, vec4(depth));
}
