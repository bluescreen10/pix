#version 460
#extension GL_EXT_nonuniform_qualifier : require
#extension GL_EXT_scalar_block_layout : require
#extension GL_GOOGLE_include_directive : require

// env_prefilter — one mip of an environment's prefiltered reflections, a cube: the
// environment as a surface of that mip's roughness mirrors it, for every direction. Mip
// 0, roughness 0, is the environment itself, read from its equirectangular image; each
// mip after it blurs by a GGX lobe, importance sampled, reading the mip before it rather
// than the environment, so that a few dozen samples land on an image already smooth
// enough not to sparkle. Each lobe is only what its mip adds to the blur of the one
// before (see environmentMipBlurs). One invocation writes one texel of one face.
#include "bindless.glsl"
#include "env_sampling.glsl"

layout(local_size_x = 8, local_size_y = 8) in;

// Matches envPrefilterRoot in environment.go.
layout(push_constant, scalar) uniform PC {
    uint source;    // the image to blur: the environment for mip 0, else this cube
    uint target;    // the storage view of the mip being written
    uint samp;
    float blurRoughness; // the GGX roughness this mip blurs the one before it by
    uint size;      // the mip's face size in texels
    uint sourceIsCube; // 0 for the equirectangular environment, 1 for this cube
    uint pad0;
    uint pad1;
} pc;

const uint SAMPLES = 64u;

// sourceLod is the level of the source with texels the size of this mip's texel at uv,
// which a trilinear tap there averages the source over: for a blurred mip, the mip
// before it, twice as fine — whose texels the tap, at this mip's texel centre, averages
// 2x2 of exactly; for mip 0, the environment's equirectangular image at whatever,
// fractional, level matches the cube texel there. A tap at the environment's full size
// would read two of the dozens of texels a mip-0 texel covers in a large panorama, and
// keep or lose a small bright sun by where it fell. A source with fewer levels — a sky
// drawn into a single-level image — clamps to its last.
float sourceLod(vec2 uv) {
    if (pc.sourceIsCube != 0u) {
        float sourceSize = float(textureSize(samplerCube(gTexturesCube[nonuniformEXT(pc.source)], gSamplers[nonuniformEXT(pc.samp)]), 0).x);
        return max(log2(sourceSize / float(pc.size)) - 1.0, 0.0);
    }
    // A cube texel's angle across shrinks from a face's centre to its corners: it is
    // the square root of its solid angle, (2 / size)^2 / (1 + s^2 + t^2)^(3/2). An
    // equirectangular texel spans 2 pi / width across. The grids do not line up, so a
    // tap finer than the cube texel would read whichever texels under it it landed on.
    vec2 st = uv * 2.0 - 1.0;
    float texelAngle = (2.0 / float(pc.size)) * pow(1.0 + dot(st, st), -0.75);
    float width = float(textureSize(sampler2D(gTextures[nonuniformEXT(pc.source)], gSamplers[nonuniformEXT(pc.samp)]), 0).x);
    return max(log2(texelAngle * width / (2.0 * ENV_PI)), 0.0);
}

vec3 sourceAt(vec3 dir, float lod) {
    if (pc.sourceIsCube != 0u) {
        return textureLod(samplerCube(gTexturesCube[nonuniformEXT(pc.source)], gSamplers[nonuniformEXT(pc.samp)]), dir, lod).rgb;
    }
    return textureLod(sampler2D(gTextures[nonuniformEXT(pc.source)], gSamplers[nonuniformEXT(pc.samp)]), equirectUV(dir), lod).rgb;
}

void main() {
    uvec3 id = gl_GlobalInvocationID;
    if (any(greaterThanEqual(id.xy, uvec2(pc.size)))) {
        return;
    }
    vec2 uv = (vec2(id.xy) + 0.5) / float(pc.size);
    vec3 N = cubeDirection(id.z, uv);

    float lod = sourceLod(uv);
    vec3 color = sourceAt(N, lod);
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
                color += sourceAt(L, lod) * NdotL;
                weight += NdotL;
            }
        }
        color /= max(weight, 1e-4);
    }
    imageStore(gImagesCube[nonuniformEXT(pc.target)], ivec3(id), vec4(color, 1.0));
}
