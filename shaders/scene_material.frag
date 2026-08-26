#version 460
#extension GL_EXT_buffer_reference : require
#extension GL_EXT_buffer_reference2 : require
#extension GL_EXT_scalar_block_layout : require
#extension GL_EXT_nonuniform_qualifier : require
#extension GL_EXT_shader_explicit_arithmetic_types_int64 : require

// Bindless heap (set 0): sampled images at binding 0, samplers at binding 2.
layout(set = 0, binding = 0) uniform texture2D gTextures[];
layout(set = 0, binding = 2) uniform sampler gSamplers[];

struct Material {
    vec4 baseColor;
    uint colorMap;
    uint samp;
    uint flags;
    uint pad;
};
layout(buffer_reference, scalar) readonly buffer MatBuf { Material v[]; };

// Same layout as DrawRoot in scene_draw.vert; only materials is read here (the
// vertex-stage pointers are opaque addresses).
layout(buffer_reference, scalar) readonly buffer DrawRoot {
    mat4 viewProj;
    uint64_t pos;
    uint64_t attr;
    uint64_t descs;
    uint64_t models;
    uint64_t drawables;
    uint64_t visible;
    MatBuf materials;
    uint regionBase;
};
layout(push_constant) uniform PC { DrawRoot root; } pc;

layout(location = 0) in vec3 vColor;
layout(location = 1) in vec2 vUV;
layout(location = 2) flat in uint vMat;
layout(location = 0) out vec4 outColor;

const uint MAT_COLOR_MAP = 1u;

void main() {
    Material mat = pc.root.materials.v[vMat];
    vec4 col = mat.baseColor * vec4(vColor, 1.0);
    if ((mat.flags & MAT_COLOR_MAP) != 0u) {
        col *= texture(sampler2D(gTextures[nonuniformEXT(mat.colorMap)], gSamplers[nonuniformEXT(mat.samp)]), vUV);
    }
    outColor = col;
}
