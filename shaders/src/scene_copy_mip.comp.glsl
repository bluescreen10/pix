#version 460
#extension GL_EXT_nonuniform_qualifier : require
#extension GL_EXT_scalar_block_layout : require
#extension GL_GOOGLE_include_directive : require

// scene_copy_mip — one level of the scene copy below the first: the level before it,
// halved. Each texel is one bilinear tap at the corner the four texels it covers share,
// which averages them exactly. Rough glass reads these: the rougher, the lower the
// level, and the more what is behind it blurs.
#include "bindless.glsl"

layout(local_size_x = 8, local_size_y = 8) in;

// Matches sceneCopyMipRoot in renderer.go.
layout(push_constant, scalar) uniform PC {
    uint copy;      // the scene copy, sampled
    uint samp;      // a linear, clamping sampler
    uint target;    // the storage view of the level being written
    float sourceLod; // the level before it
    uvec2 size;     // the level's size in texels
} pc;

void main() {
    uvec2 id = gl_GlobalInvocationID.xy;
    if (any(greaterThanEqual(id, pc.size))) {
        return;
    }
    vec2 uv = (vec2(id) + 0.5) / vec2(pc.size);
    vec3 color = textureLod(sampler2D(gTextures[nonuniformEXT(pc.copy)], gSamplers[nonuniformEXT(pc.samp)]), uv, pc.sourceLod).rgb;
    imageStore(gImages[nonuniformEXT(pc.target)], ivec2(id), vec4(color, 1.0));
}
