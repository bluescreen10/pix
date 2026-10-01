// env_sampling.glsl — GGX importance sampling, shared by the passes that blur an
// environment's reflections (env_prefilter) and build the BRDF table they are weighted
// by (env_brdf). Both integrate the same lobe, so they must sample it the same way.
#ifndef PIX_ENV_SAMPLING_GLSL
#define PIX_ENV_SAMPLING_GLSL

#include "environment.glsl"

// hammersley is the i-th of n points spread evenly over the unit square, which covers
// the lobe far more evenly than as many random ones would.
vec2 hammersley(uint i, uint n) {
    uint bits = i;
    bits = (bits << 16u) | (bits >> 16u);
    bits = ((bits & 0x55555555u) << 1u) | ((bits & 0xAAAAAAAAu) >> 1u);
    bits = ((bits & 0x33333333u) << 2u) | ((bits & 0xCCCCCCCCu) >> 2u);
    bits = ((bits & 0x0F0F0F0Fu) << 4u) | ((bits & 0xF0F0F0F0u) >> 4u);
    bits = ((bits & 0x00FF00FFu) << 8u) | ((bits & 0xFF00FF00u) >> 8u);
    return vec2(float(i) / float(n), float(bits) * 2.3283064365386963e-10);
}

// importanceSampleGGX turns a point of the unit square into a half vector about N,
// spread as a GGX lobe of the given roughness is.
vec3 importanceSampleGGX(vec2 xi, vec3 N, float roughness) {
    float a = roughness * roughness;
    float phi = 2.0 * ENV_PI * xi.x;
    float cosTheta = sqrt((1.0 - xi.y) / (1.0 + (a * a - 1.0) * xi.y));
    float sinTheta = sqrt(1.0 - cosTheta * cosTheta);
    vec3 h = vec3(sinTheta * cos(phi), sinTheta * sin(phi), cosTheta);

    vec3 up = abs(N.z) < 0.999 ? vec3(0.0, 0.0, 1.0) : vec3(1.0, 0.0, 0.0);
    vec3 tangent = normalize(cross(up, N));
    vec3 bitangent = cross(N, tangent);
    return normalize(tangent * h.x + bitangent * h.y + N * h.z);
}

#endif // PIX_ENV_SAMPLING_GLSL
