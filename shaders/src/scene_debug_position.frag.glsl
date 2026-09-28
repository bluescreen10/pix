#version 460
#extension GL_EXT_buffer_reference : require
#extension GL_EXT_buffer_reference2 : require
#extension GL_EXT_scalar_block_layout : require
#extension GL_EXT_nonuniform_qualifier : require
#extension GL_EXT_shader_explicit_arithmetic_types_int64 : require

// World position, shown as its fractional part so the scene reads as a unit grid.
//
// Taken from the interpolated varying rather than reconstructed from depth and an
// inverse view-projection, which is what the G-buffer version had to do. Same picture,
// no matrix inverse, and correct for geometry the depth buffer never received.
#include "material_common.glsl"

layout(location = 0) out vec4 outColor;

void main() {
    outColor = vec4(fract(vWorldPos), 1.0);
}
