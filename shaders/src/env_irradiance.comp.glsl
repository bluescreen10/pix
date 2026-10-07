#version 460
#extension GL_EXT_buffer_reference : require
#extension GL_EXT_buffer_reference2 : require
#extension GL_EXT_nonuniform_qualifier : require
#extension GL_EXT_scalar_block_layout : require
#extension GL_EXT_shader_explicit_arithmetic_types_int64 : require
#extension GL_GOOGLE_include_directive : require

// env_irradiance — an environment's diffuse light, as nine spherical-harmonic
// coefficients (see environmentDiffuse in lighting.glsl): its sharpest reflections, the
// cube's top level, projected onto the first three bands of the basis, each texel
// weighted by the solid angle it covers. Diffuse light is the environment blurred by a
// cosine lobe, which leaves almost nothing above the second band, so nine terms hold it
// to within a few percent.
//
// One workgroup does the whole cube: a pass of a few thousand texels, run when the
// environment changes.
#include "bindless.glsl"
#include "environment.glsl"
#include "spherical_harmonics.glsl"

const uint THREADS = 64u;
layout(local_size_x = 64) in;

layout(buffer_reference, scalar) writeonly buffer Irradiance { vec3 coefficients[9]; };

// Matches envIrradianceRoot in environment.go.
layout(push_constant, scalar) uniform PC {
    Irradiance irradiance; // where the coefficients go
    uint radiance;         // the prefiltered reflections, a cube
    uint samp;
    uint size;             // the cube's face size at its top level
    uint pad0;
} pc;

shared vec3 partial[THREADS][9];

// areaElement is the solid angle the part of a cube face from its centre to (x, y), in
// [-1,1], covers: the integral of 1 / (1 + x^2 + y^2)^(3/2) over that rectangle.
float areaElement(float x, float y) {
    return atan(x * y, sqrt(x * x + y * y + 1.0));
}

// texelSolidAngle is the solid angle texel xy of a face size texels square covers.
float texelSolidAngle(uvec2 xy, float size) {
    vec2 lo = vec2(xy) / size * 2.0 - 1.0;
    vec2 hi = vec2(xy + 1u) / size * 2.0 - 1.0;
    return areaElement(lo.x, lo.y) - areaElement(lo.x, hi.y) - areaElement(hi.x, lo.y) + areaElement(hi.x, hi.y);
}

void main() {
    uint thread = gl_LocalInvocationIndex;
    float size = float(pc.size);
    uint faceTexels = pc.size * pc.size;

    vec3 sums[9];
    for (uint k = 0u; k < 9u; k++) {
        sums[k] = vec3(0.0);
    }
    for (uint i = thread; i < 6u * faceTexels; i += THREADS) {
        uint face = i / faceTexels;
        uvec2 xy = uvec2(i % pc.size, (i % faceTexels) / pc.size);
        vec3 dir = cubeDirection(face, (vec2(xy) + 0.5) / size);
        vec3 radiance = textureLod(samplerCube(gTexturesCube[nonuniformEXT(pc.radiance)], gSamplers[nonuniformEXT(pc.samp)]), dir, 0.0).rgb;
        float basis[9] = shBasis(dir);
        vec3 weighted = radiance * texelSolidAngle(xy, size);
        for (uint k = 0u; k < 9u; k++) {
            sums[k] += weighted * basis[k];
        }
    }
    for (uint k = 0u; k < 9u; k++) {
        partial[thread][k] = sums[k];
    }
    barrier();

    if (thread >= 9u) {
        return;
    }
    // Thread k sums coefficient k over every thread's share, then folds in the cosine
    // lobe's blur of its band and the 1 / pi a white diffuse surface reflects of the
    // light reaching it (see shBandScale).
    vec3 total = vec3(0.0);
    for (uint t = 0u; t < THREADS; t++) {
        total += partial[t][thread];
    }
    pc.irradiance.coefficients[thread] = total * shBandScale(thread);
}
