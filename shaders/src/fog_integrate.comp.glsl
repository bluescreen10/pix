#version 460
#extension GL_EXT_nonuniform_qualifier : require
#extension GL_EXT_scalar_block_layout : require
#extension GL_GOOGLE_include_directive : require

// fog_integrate — the second of volumetric fog's two passes. It walks each column of the
// medium fog_inject wrote, from the camera outward, accumulating the light the fog sends
// toward the camera and the fraction of what lies behind it that gets through. At each
// slice it writes the fog between the camera and that slice's far edge — (light added,
// fraction let through) — so a lit shader needs a single lookup (see applyFog).
#include "bindless.glsl"

layout(local_size_x = 8, local_size_y = 8) in;

// Matches fogIntegrateRoot in volumetric_fog.go.
layout(push_constant, scalar) uniform PC {
    uint medium;
    uint volume;
    uint samp;
    float reach;
    uvec3 size;
    uint pad0;
} pc;

void main() {
    uvec2 column = gl_GlobalInvocationID.xy;
    if (any(greaterThanEqual(column, pc.size.xy))) {
        return;
    }

    vec3 light = vec3(0.0);
    float transmittance = 1.0;
    float slices = float(pc.size.z);
    for (uint z = 0u; z < pc.size.z; z++) {
        // The slice's near and far edges, as distances from the eye (see fogVolumeSlice
        // in lighting.glsl for the spacing).
        float nearEdge = float(z) / slices;
        float farEdge = float(z + 1u) / slices;
        float depth = (farEdge * farEdge - nearEdge * nearEdge) * pc.reach;

        vec4 medium = texelFetch(sampler3D(gTextures3D[nonuniformEXT(pc.medium)], gSamplers[nonuniformEXT(pc.samp)]), ivec3(uvec3(column, z)), 0);
        float density = medium.a;
        float through = exp(-density * depth);
        // The light the slice adds, integrated over its depth rather than taken at its
        // centre and multiplied out: each bit of it is dimmed by the fog in front of it
        // within the slice. Without that, thick fog adds more light than it can let out
        // (Hillaire, "Physically Based and Unified Volumetric Rendering in Frostbite").
        vec3 added = medium.rgb * depth;
        if (density > 0.0) {
            added = medium.rgb * (1.0 - through) / density;
        }
        light += transmittance * added;
        transmittance *= through;
        imageStore(gImages3D[nonuniformEXT(pc.volume)], ivec3(uvec3(column, z)), vec4(light, transmittance));
    }
}
