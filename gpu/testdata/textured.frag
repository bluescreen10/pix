#version 460
#extension GL_EXT_buffer_reference : require
#extension GL_EXT_scalar_block_layout : require
#extension GL_EXT_nonuniform_qualifier : require
#extension GL_EXT_shader_explicit_arithmetic_types_int64 : require

// The global bindless heap (descriptor set 0): sampled images at binding 0,
// samplers at binding 2. Indexed by ids the material carries.
layout(set = 0, binding = 0) uniform texture2D gTextures[];
layout(set = 0, binding = 2) uniform sampler gSamplers[];

// Same 16-byte layout as the vertex Root; verts is opaque here (a raw address).
layout(buffer_reference, scalar) readonly buffer Root {
    uint64_t verts;
    uint tex;
    uint samp;
};
layout(push_constant) uniform PC { Root root; } pc;

layout(location = 0) in vec2 vUV;
layout(location = 0) out vec4 outColor;

void main() {
    outColor = texture(sampler2D(gTextures[pc.root.tex], gSamplers[pc.root.samp]), vUV);
}
