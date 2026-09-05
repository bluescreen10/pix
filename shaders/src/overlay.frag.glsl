#version 460
#extension GL_EXT_buffer_reference : require
#extension GL_EXT_buffer_reference2 : require
#extension GL_EXT_scalar_block_layout : require
#extension GL_EXT_nonuniform_qualifier : require
#extension GL_EXT_shader_explicit_arithmetic_types_int64 : require

// The bindless heap (same set/bindings as the scene shaders).
layout(set = 0, binding = 0) uniform texture2D gTextures[];
layout(set = 0, binding = 2) uniform sampler gSamplers[];

layout(buffer_reference, scalar) readonly buffer QuadBuf { vec4 v[]; };
layout(buffer_reference, scalar) readonly buffer Root {
    vec2 viewport;
    uint atlas;
    uint samp;
    QuadBuf quads;
};
layout(push_constant) uniform PC { Root root; } pc;

layout(location = 0) in vec4 vColor;
layout(location = 1) in vec2 vUV;
layout(location = 0) out vec4 outColor;

void main() {
    // The atlas is single-channel coverage: 1 inside a glyph, 0 outside, filtered in
    // between. It modulates alpha only, so the text keeps its colour and antialiases
    // against whatever is behind it.
    float cov = texture(sampler2D(gTextures[nonuniformEXT(pc.root.atlas)],
                                  gSamplers[nonuniformEXT(pc.root.samp)]), vUV).r;
    outColor = vec4(vColor.rgb, vColor.a * cov);
}
