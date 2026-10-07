#version 460
#extension GL_EXT_nonuniform_qualifier : require
#extension GL_EXT_scalar_block_layout : require
#extension GL_EXT_samplerless_texture_functions : require
#extension GL_GOOGLE_include_directive : require

// scene_copy — the first level of the scene copy: the opaque scene, one texel a pixel,
// the average of its samples when it is still multisampled (see
// pix.resolvesOpaquePassEarly). Materials that show what is behind them read the copy
// while they draw into the scene itself.
#include "bindless.glsl"

layout(local_size_x = 8, local_size_y = 8) in;

// Matches sceneCopyRoot in renderer.go.
layout(push_constant, scalar) uniform PC {
    uint scene;   // the scene image
    uint samples; // its samples per pixel
    uint target;  // the storage view of the copy's first level
    uint pad0;
    uvec2 size;   // the scene's size in pixels
} pc;

void main() {
    ivec2 pixel = ivec2(gl_GlobalInvocationID.xy);
    if (any(greaterThanEqual(uvec2(pixel), pc.size))) {
        return;
    }
    vec3 color;
    if (pc.samples <= 1u) {
        color = texelFetch(gTextures[nonuniformEXT(pc.scene)], pixel, 0).rgb;
    } else {
        color = vec3(0.0);
        for (int i = 0; i < int(pc.samples); i++) {
            color += texelFetch(gTexturesMS[nonuniformEXT(pc.scene)], pixel, i).rgb;
        }
        color /= float(pc.samples);
    }
    imageStore(gImages[nonuniformEXT(pc.target)], pixel, vec4(color, 1.0));
}
