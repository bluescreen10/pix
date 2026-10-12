// assao.glsl — what the ASSAO passes share. They follow Intel's Adaptive SSAO. The scene's 
// depth is split into four interleaved images, each the depth of one
// pixel of every 2x2 block of the image's own grid, so that each image is sampled with a
// pattern of its own while its reads stay together in memory; occlusion gathered in each
// with mirrored pairs of taps; each blurred; and the four put back together.
#ifndef PIX_ASSAO_GLSL
#define PIX_ASSAO_GLSL

#include "occlusion.glsl"

// interleavedPixel is the full-resolution pixel texel of interleaved image `interleaved`
// stands for: images sample a grid every `stride` pixels, each offset by half of it, across and down, by
// its place in a 2x2 block — 0 top-left, 1 top-right, 2 bottom-left, 3 bottom-right.
vec2 interleavedPixel(ivec2 texel, uint interleaved, int stride) {
    ivec2 offset = ivec2(int(interleaved) & 1, int(interleaved) >> 1) * (stride / 2);
    return vec2(texel * stride + offset) + 0.5;
}

// surfaceEdges says, for each of a texel's left, right, top and bottom neighbours, how
// much it lies on the texel's surface: 1 on it, 0 across an edge. Calculate_edges: 
// a neighbour's depth differs from the texel's by the surface's slope
// as well as by any edge, and the slope is the same either side, so a difference the
// other side's cancels is no edge; what is left counts against the texel's depth.
vec4 surfaceEdges(float center, float left, float right, float top, float bottom) {
    vec4 differences = vec4(left, right, top, bottom) - center;
    vec4 slopeCancelled = differences + differences.yxwz;
    differences = min(abs(differences), abs(slopeCancelled));
    return clamp(1.3 - differences / (center * 0.040), 0.0, 1.0);
}

// unpackEdges undoes packEdges, letting a trace through every edge: EDGE_SHARPNESS is
// at which a full edge still passes 2% of what is across it.
const float EDGE_SHARPNESS = 0.98;

vec4 unpackEdges(float packed) {
    uint bits = uint(packed * 255.5);
    vec4 edges = vec4((bits >> 6) & 3u, (bits >> 4) & 3u, (bits >> 2) & 3u, bits & 3u) / 3.0;
    return clamp(edges + (1.0 - EDGE_SHARPNESS), 0.0, 1.0);
}

#endif
