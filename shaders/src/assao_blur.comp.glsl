#version 460
#extension GL_EXT_nonuniform_qualifier : require
#extension GL_EXT_scalar_block_layout : require
#extension GL_EXT_samplerless_texture_functions : require
#extension GL_GOOGLE_include_directive : require

// Blurs one interleaved image of occlusion — the dispatch's z picks which — with the
// texel and its four neighbours, each neighbour weighed by how much it lies on the
// texel's surface (see surfaceEdges) and the texel itself half as much as a neighbour on
// it. Each surface's occlusion stays its own across a silhouette, the one in front and 
//the one behind do not mix. The edges ride along for the next blur and the reassembly.
#include "assao.glsl"

layout(push_constant, scalar) uniform PC {
    uint sources[4];
    uint targets[4];
    ivec2 interleavedSize;
} pc;

vec2 occlusionAt(uint interleaved, ivec2 texel) {
    return texelFetch(gTextures[nonuniformEXT(pc.sources[interleaved])], clamp(texel, ivec2(0), pc.interleavedSize - 1), 0).rg;
}

layout(local_size_x = 8, local_size_y = 8) in;
void main() {
    ivec2 texel = ivec2(gl_GlobalInvocationID.xy);
    uint interleaved = gl_GlobalInvocationID.z;
    if (texel.x >= pc.interleavedSize.x || texel.y >= pc.interleavedSize.y) {
        return;
    }
    vec2 center = occlusionAt(interleaved, texel);
    vec4 edges = unpackEdges(center.y);
    float sum = center.x * 0.5;
    float weight = 0.5;
    vec4 neighbours = vec4(
        occlusionAt(interleaved, texel + ivec2(-1, 0)).x,
        occlusionAt(interleaved, texel + ivec2(1, 0)).x,
        occlusionAt(interleaved, texel + ivec2(0, -1)).x,
        occlusionAt(interleaved, texel + ivec2(0, 1)).x);
    sum += dot(neighbours, edges);
    weight += dot(edges, vec4(1.0));
    imageStore(gImages[nonuniformEXT(pc.targets[interleaved])], texel, vec4(sum / weight, center.y, 0.0, 0.0));
}
