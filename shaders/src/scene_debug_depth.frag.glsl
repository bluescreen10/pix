#version 460
#extension GL_EXT_buffer_reference : require
#extension GL_EXT_buffer_reference2 : require
#extension GL_EXT_scalar_block_layout : require
#extension GL_EXT_nonuniform_qualifier : require
#extension GL_EXT_shader_explicit_arithmetic_types_int64 : require

// Depth, curved for readability: near reads dark, far reads bright.
#include "material_common.glsl"

layout(location = 0) out vec4 outColor;

void main() {
    // Reversed-Z, so d is roughly near/z: log2(d) is therefore linear in log(z), which
    // spreads any near/far ratio evenly across the range instead of saturating. A
    // plain 1-d (or its square root) is white almost everywhere once near/far passes a
    // few hundred to one, which is every scene worth inspecting.
    //
    // The max() guards log2(0) on a driver that does not clamp it to -inf cleanly.
    float d = gl_FragCoord.z;
    outColor = vec4(vec3(clamp(-log2(max(d, 1e-9)) / 20.0, 0.0, 1.0)), 1.0);
}
