#version 460
#extension GL_EXT_nonuniform_qualifier : require
#extension GL_EXT_scalar_block_layout : require
#extension GL_GOOGLE_include_directive : require

// env_prefilter — one mip of an environment's prefiltered reflections: the environment
// as a surface of that mip's roughness mirrors it, for every direction. Mip 0, roughness
// 0, is the environment itself; each mip after it blurs by a GGX lobe, importance
// sampled, reading the mip before it rather than the environment, so that a few dozen
// samples land on an image already smooth enough not to sparkle. Each lobe is only what
// its mip adds to the blur of the one before (see environmentMipBlurs).
#include "bindless.glsl"
#include "env_sampling.glsl"

layout(local_size_x = 8, local_size_y = 8) in;

// Matches envPrefilterRoot in environment.go.
layout(push_constant, scalar) uniform PC {
    uint source;    // the image to blur: the environment for mip 0, else this map
    uint target;    // the storage view of the mip being written
    uint samp;
    float blurRoughness; // the GGX roughness this mip blurs the one before it by
    uvec2 size;     // the mip's size in texels
    float sourceLod;
    uint pad0;
} pc;

const uint SAMPLES = 64u;

vec3 sourceAt(vec3 dir) {
    return textureLod(sampler2D(gTextures[nonuniformEXT(pc.source)], gSamplers[nonuniformEXT(pc.samp)]), equirectUV(dir), pc.sourceLod).rgb;
}

void main() {
    uvec2 id = gl_GlobalInvocationID.xy;
    if (any(greaterThanEqual(id, pc.size))) {
        return;
    }
    vec3 N = equirectDirection((vec2(id) + 0.5) / vec2(pc.size));

    vec3 color = sourceAt(N);
    if (pc.blurRoughness > 0.0) {
        // The usual assumption of split-sum prefiltering: the viewer looks along the
        // normal, so the reflection direction is the normal too.
        color = vec3(0.0);
        float weight = 0.0;
        for (uint i = 0u; i < SAMPLES; i++) {
            vec3 H = importanceSampleGGX(hammersley(i, SAMPLES), N, pc.blurRoughness);
            vec3 L = 2.0 * dot(N, H) * H - N;
            float NdotL = dot(N, L);
            if (NdotL > 0.0) {
                color += sourceAt(L) * NdotL;
                weight += NdotL;
            }
        }
        color /= max(weight, 1e-4);
    }
    imageStore(gImages[nonuniformEXT(pc.target)], ivec2(id), vec4(color, 1.0));
}
