#version 460
#extension GL_EXT_buffer_reference : require
#extension GL_EXT_buffer_reference2 : require
#extension GL_EXT_scalar_block_layout : require
#extension GL_EXT_nonuniform_qualifier : require
#extension GL_EXT_shader_explicit_arithmetic_types_int64 : require
#extension GL_GOOGLE_include_directive : require

// BasicMaterial (unlit): base color × vertex color × color map, plus emissive.
#include "material_common.glsl"

// Material mirrors pix.basicRecord (48 bytes).
struct Material {
    vec4 baseColor;
    vec4 emissive;
    uint colorMap;
    uint samp;
    uint flags;
    uint pad;
};
layout(buffer_reference, scalar) readonly buffer MatBuf { Material v[]; };

void main() {
    Material m = MatBuf(pc.root.materials).v[vMat];
    vec4 base = sampleBase(m.baseColor, m.flags, m.colorMap, m.samp);
    outColor = vec4(linearToSrgb(base.rgb + m.emissive.rgb), base.a);
}
