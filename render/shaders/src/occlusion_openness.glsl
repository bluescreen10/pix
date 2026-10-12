// occlusion_openness.glsl — how open the surface at a full-resolution pixel is, read
// from what the ambient occlusion pass measured: VBAO's full-resolution image, or ASSAO's
// four interleaved images, put together here (see interleavedOpenness) — which spares a
// full-resolution pass and image whose only reader would be this. It reads no depth, so
// a draw inside a pass that has the scene's depth attached can use it.
//
// A shader including this declares, before it, the extensions it relies on:
// GL_EXT_nonuniform_qualifier, GL_EXT_scalar_block_layout and
// GL_EXT_samplerless_texture_functions.
#ifndef PIX_OCCLUSION_OPENNESS_GLSL
#define PIX_OCCLUSION_OPENNESS_GLSL

#include "assao.glsl"

// OpennessSource mirrors pix.opennessSource: where the openness is, and how it is
// shaped.
struct OpennessSource {
    // openness is VBAO's full-resolution image, read when isInterleaved is 0.
    uint openness;
    // isInterleaved is 1 when the openness is in the interleaved images, which sample
    // every stride pixels (see interleavedPixel), a power of two.
    uint isInterleaved;
    uint linearSampler;
    int stride;
    uint interleavedImages[4];
    ivec2 interleavedSize;
    float intensity;
};

// The interleaved images are read in blocks: a block is halfStride pixels square, and
// each image has a texel in every other block across and down (see interleavedPixel) —
// image i's texel t in block 2t + (i & 1, i >> 1).

// opennessNear reads interleaved image `interleaved` at block, filtered between the
// texels around it.
float opennessNear(OpennessSource source, uint interleaved, vec2 block) {
    vec2 place = vec2(interleaved & 1u, interleaved >> 1u);
    vec2 uv = ((block - place) * 0.5 + 0.5) / vec2(source.interleavedSize);
    return textureLod(sampler2D(gTextures[nonuniformEXT(source.interleavedImages[interleaved])], gSamplers[nonuniformEXT(source.linearSampler)]), uv, 0.0).r;
}

// interleavedOpenness puts the openness at pixel together from the four interleaved
// images The image with a texel in the pixel's block gives it exactly. Each of the other 
// three has its texels in the blocks beside it — left and right, above and below, or on 
// the diagonals — and is read between those two (or four), moved towards the side with 
// no edge, and counts for as much as the texel lies on one surface with its neighbours
// that way: across an outline, the surface behind does not lend the one in front its 
// occlusion. Each is read on the texels' own row or column, never past them: a pixel
// off it would reach a texel two blocks away, across an outline the edges know nothing of.
float interleavedOpenness(OpennessSource source, ivec2 pixel) {
    // Shifts, not divisions: the stride is a power of two, and integer division by a
    // value the compiler cannot see is many instructions on a GPU — this draw ran in 0.33
    // ms at 2560x1440 dividing, and 0.19 ms shifting.
    int strideShift = findMSB(source.stride);
    int halfStride = source.stride >> 1;
    ivec2 ownBlock = pixel >> (strideShift - 1);
    ivec2 place = ownBlock & 1;
    uint own = uint(place.x + place.y * 2);
    uint across = uint((1 - place.x) + place.y * 2);
    uint down = uint(place.x + (1 - place.y) * 2);
    uint diagonal = uint((1 - place.x) + (1 - place.y) * 2);

    ivec2 texel = clamp(pixel >> strideShift, ivec2(0), source.interleavedSize - 1);
    vec2 center = texelFetch(gTextures[nonuniformEXT(source.interleavedImages[own])], texel, 0).rg;
    vec4 edges = unpackEdges(center.y);

    // Where the pixel lies within its block, smoothly, moved towards the side with no
    // edge — right minus left, bottom minus top — and kept between the blocks beside it.
    vec2 block = (vec2(pixel) + 0.5) / float(halfStride) - 0.5;
    vec2 awayFromEdges = vec2(edges.y - edges.x, edges.w - edges.z);
    vec2 shiftedBlock = clamp(block + awayFromEdges, vec2(ownBlock) - 1.0, vec2(ownBlock) + 1.0);
    float acrossValue = opennessNear(source, across, vec2(shiftedBlock.x, ownBlock.y));
    float downValue = opennessNear(source, down, vec2(ownBlock.x, shiftedBlock.y));
    float diagonalValue = opennessNear(source, diagonal, shiftedBlock);

    vec4 weights;
    weights.x = 1.0;
    weights.y = (edges.x + edges.y) * 0.5;
    weights.z = (edges.z + edges.w) * 0.5;
    weights.w = (weights.y + weights.z) * 0.5;
    return dot(vec4(center.x, acrossValue, downValue, diagonalValue), weights) / dot(weights, vec4(1.0));
}

// opennessAt is how open the surface at pixel is: 1 where nothing occludes it, down to 0,
// shaped by the settings' intensity.
float opennessAt(OpennessSource source, ivec2 pixel) {
    float share;
    if (source.isInterleaved != 0u) {
        share = interleavedOpenness(source, pixel);
    } else {
        share = texelFetch(gTextures[nonuniformEXT(source.openness)], pixel, 0).r;
    }
    return pow(clamp(share, 0.0, 1.0), source.intensity);
}

#endif
