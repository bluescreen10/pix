#version 460
#extension GL_EXT_nonuniform_qualifier : require
#extension GL_EXT_scalar_block_layout : require
#extension GL_GOOGLE_include_directive : require

// image_write fills a writable 2D texture through the heap's storage array with one
// colour, orange — (1, 0.5, 0), which nothing else in its test draws.
#include "../shaders/src/bindless.glsl"

layout(push_constant, scalar) uniform PC {
    uint image;
    uint side;
    uint pad0;
    uint pad1;
} pc;

layout(local_size_x = 8, local_size_y = 8) in;
void main() {
    uvec2 p = gl_GlobalInvocationID.xy;
    if (any(greaterThanEqual(p, uvec2(pc.side)))) {
        return;
    }
    imageStore(gImages[nonuniformEXT(pc.image)], ivec2(p), vec4(1.0, 0.5, 0.0, 1.0));
}
