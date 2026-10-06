#version 460
#extension GL_EXT_nonuniform_qualifier : require
#extension GL_EXT_scalar_block_layout : require
#extension GL_EXT_samplerless_texture_functions : require
#extension GL_GOOGLE_include_directive : require

// Smooths what the VBAO pass measured over each texel and its eight neighbours, each
// counting for as much as it lies on the texel's surface, by the edges that pass wrote
// (XeGTAO_Denoise; Intel, MIT, github.com/GameTechDev/XeGTAO). Run a few times over; each
// time but the last weighs the texel itself less, to smooth more. The edges ride along
// for the next time.
#include "occlusion.glsl"

layout(push_constant, scalar) uniform PC {
    uint source;
    uint target;
    ivec2 size;
    // centerWeight is how much the texel itself counts, against at most 1 for each
    // neighbour.
    float centerWeight;
    float pad0;
    float pad1;
    float pad2;
} pc;

// DIAGONAL_WEIGHT is how much a diagonal neighbour counts at most, against 1 for one
// beside the texel.
const float DIAGONAL_WEIGHT = 0.85 * 0.5;

// measuredAt is the visibility and packed edges the VBAO pass left at texel.
vec2 measuredAt(ivec2 texel) {
    return texelFetch(gTextures[nonuniformEXT(pc.source)], clamp(texel, ivec2(0), pc.size - 1), 0).rg;
}

// edgesOf unpacks packEdges: left, right, top, bottom.
vec4 edgesOf(float packed) {
    uint bits = uint(packed * 255.5);
    return vec4((bits >> 6) & 3u, (bits >> 4) & 3u, (bits >> 2) & 3u, bits & 3u) / 3.0;
}

layout(local_size_x = 8, local_size_y = 8) in;
void main() {
    ivec2 texel = ivec2(gl_GlobalInvocationID.xy);
    if (texel.x >= pc.size.x || texel.y >= pc.size.y) {
        return;
    }
    vec2 center = measuredAt(texel);
    vec2 left = measuredAt(texel + ivec2(-1, 0));
    vec2 right = measuredAt(texel + ivec2(1, 0));
    vec2 up = measuredAt(texel + ivec2(0, -1));
    vec2 down = measuredAt(texel + ivec2(0, 1));
    vec4 leftEdges = edgesOf(left.y);
    vec4 rightEdges = edgesOf(right.y);
    vec4 upEdges = edgesOf(up.y);
    vec4 downEdges = edgesOf(down.y);

    // An edge counts from either side: the texel's own, and its neighbour's back.
    vec4 edges = edgesOf(center.y) * vec4(leftEdges.y, rightEdges.x, upEdges.w, downEdges.z);
    // A texel edged on three or four sides lets a little through anyway, against
    // aliasing.
    const float LEAK_THRESHOLD = 2.5;
    const float LEAK_STRENGTH = 0.5;
    float edginess = clamp(4.0 - LEAK_THRESHOLD - dot(edges, vec4(1.0)), 0.0, 1.0) / (4.0 - LEAK_THRESHOLD) * LEAK_STRENGTH;
    edges = clamp(edges + edginess, 0.0, 1.0);

    // A diagonal neighbour counts as far as either way round to it stays on the surface.
    float weightUpLeft = DIAGONAL_WEIGHT * (edges.x * leftEdges.z + edges.z * upEdges.x);
    float weightUpRight = DIAGONAL_WEIGHT * (edges.z * upEdges.y + edges.y * rightEdges.z);
    float weightDownLeft = DIAGONAL_WEIGHT * (edges.w * downEdges.x + edges.x * leftEdges.w);
    float weightDownRight = DIAGONAL_WEIGHT * (edges.y * rightEdges.w + edges.w * downEdges.y);

    float sum = center.x * pc.centerWeight;
    float weight = pc.centerWeight;
    sum += dot(vec4(left.x, right.x, up.x, down.x), edges);
    weight += dot(edges, vec4(1.0));
    vec4 diagonals = vec4(measuredAt(texel + ivec2(-1, -1)).x, measuredAt(texel + ivec2(1, -1)).x,
        measuredAt(texel + ivec2(-1, 1)).x, measuredAt(texel + ivec2(1, 1)).x);
    vec4 diagonalWeights = vec4(weightUpLeft, weightUpRight, weightDownLeft, weightDownRight);
    sum += dot(diagonals, diagonalWeights);
    weight += dot(diagonalWeights, vec4(1.0));
    imageStore(gImages[nonuniformEXT(pc.target)], texel, vec4(sum / weight, center.y, 0.0, 0.0));
}
