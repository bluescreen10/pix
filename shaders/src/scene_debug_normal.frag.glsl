#version 460
#extension GL_EXT_buffer_reference : require
#extension GL_EXT_buffer_reference2 : require
#extension GL_EXT_scalar_block_layout : require
#extension GL_EXT_nonuniform_qualifier : require
#extension GL_EXT_shader_explicit_arithmetic_types_int64 : require

// World-space normals, remapped from [-1,1] to [0,1] so they are visible as colour.
//
// A real geometry pass over the material's own vertex stage, not a re-read of a stored
// target: the normal comes straight from the varying scene_draw.vert interpolates, so
// this shows what shading would actually receive — for every material type, since
// nothing here touches a material record.
#include "material_common.glsl"

layout(location = 0) out vec4 outColor;

void main() {
    // Renormalised because interpolation across a triangle shortens it, and an
    // unnormalised normal reads as a darker face rather than a differently-oriented
    // one — which would look like a shading bug that isn't there.
    outColor = vec4(normalize(vNormal) * 0.5 + 0.5, 1.0);
}
