// material_common.glsl — shared by the scene material fragment shaders (forward AND
// G-buffer fill). It pulls in the bindless heap + light table (lighting.glsl), then
// declares the DrawRoot push constant (materials is an opaque address — each shader
// casts it to its own per-type record buffer) and the vertex→fragment varyings.
//
// It deliberately does NOT declare a color output: forward shaders write a single
// outColor, the G-buffer fill writes several MRT targets, so each fragment shader
// declares its own outputs.
#ifndef MATERIAL_COMMON_GLSL
#define MATERIAL_COMMON_GLSL

// Bindless heap, light table, shadow sampling and linearToSrgb.
#include "lighting.glsl"

const uint MAT_COLOR_MAP = 1u;
const uint FLAG_RECEIVES_SHADOW = 4u; // Drawable flag (mirrors pix.DrawableReceivesShadow)

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
    uint shadowSampler; // bindless index of the PCF comparison sampler
    uint spad0;
    uint spad1;
    uint spad2;
};
layout(push_constant) uniform PC { DrawRoot root; } pc;

// Vertex → fragment varyings (produced by scene_draw.vert).
layout(location = 0) in vec3 vColor;
layout(location = 1) in vec2 vUV;
layout(location = 2) flat in uint vMat;
layout(location = 3) in vec3 vWorldPos;
layout(location = 4) in vec3 vNormal;
layout(location = 5) flat in uint vFlags; // drawable flags (FLAG_RECEIVES_SHADOW, …)

// sampleBase returns base color × vertex color, modulated by the color map when the
// MAT_COLOR_MAP flag is set. Fields come from the shader's own material record.
vec4 sampleBase(vec4 base, uint flags, uint colorMap, uint samp) {
    vec4 c = base * vec4(vColor, 1.0);
    if ((flags & MAT_COLOR_MAP) != 0u) {
        c *= texture(sampler2D(gTextures[nonuniformEXT(colorMap)], gSamplers[nonuniformEXT(samp)]), vUV);
    }
    return c;
}

#endif // MATERIAL_COMMON_GLSL
