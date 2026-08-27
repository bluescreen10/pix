// material_common.glsl — shared by the scene material fragment shaders. It declares
// the bindless heap, the light table, the DrawRoot push constant (materials is an
// opaque address — each shader casts it to its own per-type record buffer), the
// vertex→fragment varyings, and small helpers. Each fragment shader #includes this,
// defines its own Material record, and implements main().
#ifndef MATERIAL_COMMON_GLSL
#define MATERIAL_COMMON_GLSL

// Bindless heap (set 0): sampled images at binding 0, samplers at binding 2.
layout(set = 0, binding = 0) uniform texture2D gTextures[];
layout(set = 0, binding = 2) uniform sampler gSamplers[];

const uint MAX_DIR = 4u;
const uint MAX_POINT = 16u;
const uint MAT_COLOR_MAP = 1u;

struct DirLight { vec4 dir; vec4 color; };   // dir.xyz travels; color.w = intensity
struct PointLight { vec4 pos; vec4 color; }; // pos.xyz world, pos.w = range; color.w = intensity
layout(buffer_reference, scalar) readonly buffer LightBuf {
    vec4 ambient;
    uint numDir;
    uint numPoint;
    uint pad0;
    uint pad1;
    DirLight dirs[MAX_DIR];
    PointLight points[MAX_POINT];
};

// DrawRoot mirrors pix.drawRoot. Buffer pointers are opaque uint64 (the vertex stage
// uses pos/attr/descs/models/drawables/visible; the fragment stage casts `materials`
// to its own per-type record buffer and reads `lights`). There is no regionBase:
// each indirect command sets firstInstance, so gl_InstanceIndex indexes visible[].
layout(buffer_reference, scalar) readonly buffer DrawRoot {
    mat4 viewProj;
    uint64_t pos;
    uint64_t attr;
    uint64_t descs;
    uint64_t models;
    uint64_t drawables;
    uint64_t visible;
    uint64_t materials;
    LightBuf lights;
    vec4 eye;
};
layout(push_constant) uniform PC { DrawRoot root; } pc;

// Vertex → fragment varyings (produced by scene_draw.vert).
layout(location = 0) in vec3 vColor;
layout(location = 1) in vec2 vUV;
layout(location = 2) flat in uint vMat;
layout(location = 3) in vec3 vWorldPos;
layout(location = 4) in vec3 vNormal;
layout(location = 0) out vec4 outColor;

// sampleBase returns base color × vertex color, modulated by the color map when the
// MAT_COLOR_MAP flag is set. Fields come from the shader's own material record.
vec4 sampleBase(vec4 base, uint flags, uint colorMap, uint samp) {
    vec4 c = base * vec4(vColor, 1.0);
    if ((flags & MAT_COLOR_MAP) != 0u) {
        c *= texture(sampler2D(gTextures[nonuniformEXT(colorMap)], gSamplers[nonuniformEXT(samp)]), vUV);
    }
    return c;
}

// linearToSrgb encodes a linear color to sRGB for display.
vec3 linearToSrgb(vec3 c) {
    c = clamp(c, 0.0, 1.0);
    return mix(1.055 * pow(c, vec3(1.0 / 2.4)) - 0.055, c * 12.92, lessThanEqual(c, vec3(0.0031308)));
}

#endif // MATERIAL_COMMON_GLSL
