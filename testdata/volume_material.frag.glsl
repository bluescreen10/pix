#version 460
#extension GL_EXT_buffer_reference : require
#extension GL_EXT_buffer_reference2 : require
#extension GL_EXT_scalar_block_layout : require
#extension GL_EXT_nonuniform_qualifier : require
#extension GL_EXT_shader_explicit_arithmetic_types_int64 : require
#extension GL_GOOGLE_include_directive : require

// volume_material shows one depth of a 3D texture, unlit: the colour at the volume's
// centre in x and y, at the depth its record asks for.
#include "../shaders/src/material_common.glsl"

layout(location = 0) out vec4 outColor;

// Material mirrors volumeRecord in writable_texture_test.go.
struct Material {
    uint volume;
    uint samp;
    float depth;
    uint pad;
};
layout(buffer_reference, scalar) readonly buffer MatBuf { Material v[]; };

void main() {
    Material m = MatBuf(pc.materials).v[vMat];
    outColor = texture(sampler3D(gTextures3D[nonuniformEXT(m.volume)], gSamplers[nonuniformEXT(m.samp)]), vec3(0.5, 0.5, m.depth));
}
