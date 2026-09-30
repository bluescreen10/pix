#version 460
#extension GL_EXT_nonuniform_qualifier : require
#extension GL_EXT_scalar_block_layout : require
#extension GL_GOOGLE_include_directive : require

// volume_write fills a writable 3D texture through the heap's storage array: the near
// half of its depth red, the far half green.
#include "../shaders/src/bindless.glsl"

layout(push_constant, scalar) uniform PC {
    uint volume;
    uint size;
    uint pad0;
    uint pad1;
} pc;

layout(local_size_x = 4, local_size_y = 4, local_size_z = 4) in;
void main() {
    uvec3 p = gl_GlobalInvocationID;
    if (any(greaterThanEqual(p, uvec3(pc.size)))) {
        return;
    }
    vec4 color = p.z < pc.size / 2u ? vec4(1.0, 0.0, 0.0, 1.0) : vec4(0.0, 1.0, 0.0, 1.0);
    imageStore(gImages3D[nonuniformEXT(pc.volume)], ivec3(p), color);
}
