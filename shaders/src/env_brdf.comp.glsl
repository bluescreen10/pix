#version 460
#extension GL_EXT_nonuniform_qualifier : require
#extension GL_EXT_scalar_block_layout : require
#extension GL_GOOGLE_include_directive : require

// env_brdf — the BRDF table of split-sum environment lighting (Karis, "Real Shading in
// Unreal Engine 4"): for each view angle (u, as NdotV) and roughness (v), the scale and
// bias that turn a surface's f0 into how much of the prefiltered environment it
// reflects. It depends on nothing in a scene, so a renderer computes it once.
#include "bindless.glsl"
#include "env_sampling.glsl"

layout(local_size_x = 8, local_size_y = 8) in;

// Matches envBRDFRoot in environment.go.
layout(push_constant, scalar) uniform PC {
    uint table;
    uint pad0;
    uint pad1;
    uint pad2;
} pc;

const uint SAMPLES = 128u;

// geometrySchlick is Schlick's GGX shadowing for one direction, with the k image-based
// lighting uses (roughness² / 2).
float geometrySchlick(float NdotX, float roughness) {
    float k = roughness * roughness / 2.0;
    return NdotX / (NdotX * (1.0 - k) + k);
}

void main() {
    uvec2 id = gl_GlobalInvocationID.xy;
    if (any(greaterThanEqual(id, uvec2(ENV_BRDF_SIZE)))) {
        return;
    }
    float NdotV = (float(id.x) + 0.5) / ENV_BRDF_SIZE;
    float roughness = (float(id.y) + 0.5) / ENV_BRDF_SIZE;
    vec3 V = vec3(sqrt(1.0 - NdotV * NdotV), 0.0, NdotV);
    vec3 N = vec3(0.0, 0.0, 1.0);

    float scale = 0.0;
    float bias = 0.0;
    for (uint i = 0u; i < SAMPLES; i++) {
        vec3 H = importanceSampleGGX(hammersley(i, SAMPLES), N, roughness);
        vec3 L = 2.0 * dot(V, H) * H - V;
        float NdotL = max(L.z, 0.0);
        if (NdotL <= 0.0) {
            continue;
        }
        float NdotH = max(H.z, 0.0);
        float VdotH = max(dot(V, H), 0.0);
        float visibility = geometrySchlick(NdotV, roughness) * geometrySchlick(NdotL, roughness) * VdotH / max(NdotH * NdotV, 1e-4);
        float fresnel = pow(1.0 - VdotH, 5.0);
        scale += (1.0 - fresnel) * visibility;
        bias += fresnel * visibility;
    }
    imageStore(gImages[nonuniformEXT(pc.table)], ivec2(id), vec4(scale, bias, 0.0, 1.0) / vec4(float(SAMPLES), float(SAMPLES), 1.0, 1.0));
}
