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
const uint MAX_SPOT = 8u;
const uint MAT_COLOR_MAP = 1u;
const uint FLAG_RECEIVES_SHADOW = 4u; // Drawable flag (mirrors pix.DrawableReceivesShadow)
const uint NO_SHADOW = 0xFFFFFFFFu;   // shadowMap sentinel: light casts no shadow

// dir.xyz travels; color.w = intensity. shadowVP maps world→light clip and shadowMap
// is the depth map's bindless index (NO_SHADOW when the light casts none).
struct DirLight { vec4 dir; vec4 color; mat4 shadowVP; uint shadowMap; uint pad0; uint pad1; uint pad2; };
// pos.xyz world, pos.w = range; color.w = intensity. shadowVP/shadowMap are the six
// cube faces (+X,-X,+Y,-Y,+Z,-Z); shadowMap[f] = NO_SHADOW when the light casts none.
struct PointLight { vec4 pos; vec4 color; mat4 shadowVP[6]; uint shadowMap[6]; uint pad0; uint pad1; };
// pos.xyz world/pos.w range; dir.xyz cone axis/dir.w cosOuter; color.w intensity.
struct SpotLight { vec4 pos; vec4 dir; vec4 color; mat4 shadowVP; float cosInner; uint shadowMap; uint pad0; uint pad1; };
layout(buffer_reference, scalar) readonly buffer LightBuf {
    vec4 ambient;
    uint numDir;
    uint numPoint;
    uint numSpot;
    uint pad0;
    DirLight dirs[MAX_DIR];
    PointLight points[MAX_POINT];
    SpotLight spots[MAX_SPOT];
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

// shadowFactor returns the lit fraction (1 = fully lit, 0 = fully shadowed) for a light
// whose depth map is shadowVP/shadowMap, using hardware PCF through a comparison sampler
// (shadowSamp is the sampler's heap index). Works for both orthographic (directional)
// and perspective (spot) maps — the perspective divide handles both. Points outside the
// map or beyond the far plane are treated as lit. A small constant bias fights acne.
float shadowFactor(mat4 shadowVP, uint shadowMap, vec3 worldPos, uint shadowSamp) {
    if (shadowMap == NO_SHADOW) return 1.0;
    vec4 c = shadowVP * vec4(worldPos, 1.0);
    if (c.w <= 0.0) return 1.0;
    vec3 ndc = c.xyz / c.w;              // Vulkan: xy in [-1,1], z in [0,1]
    vec2 uv = ndc.xy * 0.5 + 0.5;        // framebuffer/texture origin both top-left
    if (ndc.z > 1.0 || any(lessThan(uv, vec2(0.0))) || any(greaterThan(uv, vec2(1.0)))) {
        return 1.0;
    }
    float ref = ndc.z - 0.0015;
    return texture(sampler2DShadow(gTextures[nonuniformEXT(shadowMap)], gSamplers[nonuniformEXT(shadowSamp)]), vec3(uv, ref));
}

// pointShadowFactor picks the cube face for the light→fragment direction (dominant
// axis, matching pix's cubeFaceDirs order +X,-X,+Y,-Y,+Z,-Z) and samples that face's
// perspective shadow map. 1.0 when the light casts no shadow.
float pointShadowFactor(PointLight pl, vec3 worldPos, uint shadowSamp) {
    vec3 v = worldPos - pl.pos.xyz;
    vec3 a = abs(v);
    uint face;
    if (a.x >= a.y && a.x >= a.z) {
        face = v.x > 0.0 ? 0u : 1u;
    } else if (a.y >= a.z) {
        face = v.y > 0.0 ? 2u : 3u;
    } else {
        face = v.z > 0.0 ? 4u : 5u;
    }
    return shadowFactor(pl.shadowVP[face], pl.shadowMap[face], worldPos, shadowSamp);
}

// spotAttenuation is a spot light's distance × cone falloff for a world position (0
// outside the outer cone / beyond range). Ldir points from the fragment to the light.
float spotAttenuation(SpotLight sl, vec3 worldPos, vec3 Ldir, float dist) {
    float range = max(sl.pos.w, 1e-4);
    float atten = clamp(1.0 - dist / range, 0.0, 1.0);
    atten *= atten;
    float cosA = dot(-Ldir, sl.dir.xyz);                  // fragment direction vs cone axis
    atten *= smoothstep(sl.dir.w, sl.cosInner, cosA);     // dir.w = cosOuter
    return atten;
}

// linearToSrgb encodes a linear color to sRGB for display.
vec3 linearToSrgb(vec3 c) {
    c = clamp(c, 0.0, 1.0);
    return mix(1.055 * pow(c, vec3(1.0 / 2.4)) - 0.055, c * 12.92, lessThanEqual(c, vec3(0.0031308)));
}

#endif // MATERIAL_COMMON_GLSL
