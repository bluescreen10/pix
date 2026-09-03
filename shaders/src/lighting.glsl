// lighting.glsl — the light table + shadow sampling, standalone for the deferred
// Lighting() passes (a fullscreen shader, no vertex varyings, no per-drawable material
// record — unlike material_common.glsl's forward shape). Deliberately a copy of
// material_common.glsl's light-table declarations rather than a shared include: the
// forward path is load-bearing and already tested, and duplicating ~60 lines here
// keeps this change from touching it at all. A future cleanup can fold both into one
// header once both paths are proven out.
#ifndef PIX_LIGHTING_GLSL
#define PIX_LIGHTING_GLSL

// Bindless heap (set 0): sampled images at binding 0, samplers at binding 2.
layout(set = 0, binding = 0) uniform texture2D gTextures[];
layout(set = 0, binding = 2) uniform sampler gSamplers[];

const uint MAX_DIR = 4u;
const uint MAX_POINT = 16u;
const uint MAX_SPOT = 8u;
// Fog modes, mirroring pix.fogNone/fogLinear/fogExp2.
const uint FOG_NONE = 0u;
const uint FOG_LINEAR = 1u;
const uint FOG_EXP2 = 2u;

const uint NO_SHADOW = 0xFFFFFFFFu; // shadowMap sentinel: light casts no shadow

struct DirLight { vec4 dir; vec4 color; mat4 shadowVP; uint shadowMap; float shadowBias; uint pad0; uint pad1; };
struct PointLight { vec4 pos; vec4 color; mat4 shadowVP[6]; uint shadowMap[6]; float shadowBias; uint pad0; };
struct SpotLight { vec4 pos; vec4 dir; vec4 color; mat4 shadowVP; float cosInner; uint shadowMap; float shadowBias; uint pad0; };
layout(buffer_reference, scalar) readonly buffer LightBuf {
    vec4 ambient;
    vec4 fogColor;  // rgb = fog colour, w = FOG_* mode
    vec4 fogParams; // (near, far, density, _)
    uint numDir;
    uint numPoint;
    uint numSpot;
    uint pad0;
    DirLight dirs[MAX_DIR];
    PointLight points[MAX_POINT];
    SpotLight spots[MAX_SPOT];
};

// shadowFactor returns the lit fraction (1 = fully lit, 0 = fully shadowed) for a light
// whose depth map is shadowVP/shadowMap, using hardware PCF through a comparison
// sampler. Works for both orthographic (directional) and perspective (spot) maps.
//
// bias is in the shadow camera's NORMALIZED depth units. It cannot be a shared
// constant: a directional light's orthographic depth range spans the whole scene, so
// the same NDC value means a completely different world distance there than it does
// for a spot/point light whose range is local. Directional lights carry a bias
// computed from their fit; spot/point carry one derived from the light's range. Every
// light type carries its own, so LightShadow.Bias means something for all of them.
float shadowFactor(mat4 shadowVP, uint shadowMap, vec3 worldPos, uint shadowSamp, float bias) {
    if (shadowMap == NO_SHADOW) return 1.0;
    vec4 c = shadowVP * vec4(worldPos, 1.0);
    if (c.w <= 0.0) return 1.0;
    vec3 ndc = c.xyz / c.w;
    vec2 uv = ndc.xy * 0.5 + 0.5;
    // Reversed-Z: the far plane is 0 and nearer is GREATER, so "beyond the shadow
    // camera's far plane" is z < 0 rather than z > 1.
    if (ndc.z < 0.0 || any(lessThan(uv, vec2(0.0))) || any(greaterThan(uv, vec2(1.0)))) {
        return 1.0;
    }
    // Bias pushes the reference toward the light to avoid self-shadowing acne.
    // Under reversed-Z "toward the light" is a LARGER depth, so this adds where a
    // conventional depth buffer would subtract. The comparison sampler is
    // GreaterEqual to match (see Renderer.prepareShadows).
    float ref = ndc.z + bias;
    return texture(sampler2DShadow(gTextures[nonuniformEXT(shadowMap)], gSamplers[nonuniformEXT(shadowSamp)]), vec3(uv, ref));
}

// pointShadowFactor picks the cube face for the light→fragment direction (dominant
// axis, matching pix's cubeFaceDirs order +X,-X,+Y,-Y,+Z,-Z) and samples that face.
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
    return shadowFactor(pl.shadowVP[face], pl.shadowMap[face], worldPos, shadowSamp, pl.shadowBias);
}

// spotAttenuation is a spot light's distance × cone falloff for a world position.
float spotAttenuation(SpotLight sl, vec3 worldPos, vec3 Ldir, float dist) {
    float range = max(sl.pos.w, 1e-4);
    float atten = clamp(1.0 - dist / range, 0.0, 1.0);
    atten *= atten;
    float cosA = dot(-Ldir, sl.dir.xyz);
    atten *= smoothstep(sl.dir.w, sl.cosInner, cosA);
    return atten;
}

// linearToSrgb encodes a linear color to sRGB for display.
vec3 linearToSrgb(vec3 c) {
    c = clamp(c, 0.0, 1.0);
    return mix(1.055 * pow(c, vec3(1.0 / 2.4)) - 0.055, c * 12.92, lessThanEqual(c, vec3(0.0031308)));
}

// applyFog blends a LINEAR-space shaded colour toward the scene's fog colour by
// distance from the eye. Call it before the sRGB encode: fog is a physical blend
// between the surface and the medium in front of it, and doing it after the encode
// washes the result out.
//
// The returned value is the fogged colour; a scene with no fog returns lit unchanged
// (one compare, and the branch is uniform across the draw).
//
// fogColor/fogParams are passed by value rather than the LightBuf itself: a
// buffer_reference cannot cross a function parameter without dropping its readonly
// qualifier, the same reason shadeSurface reads the table from pc.root directly.
vec3 applyFog(vec3 lit, vec3 worldPos, vec3 eye, vec4 fogColor, vec4 fogParams) {
    uint mode = uint(fogColor.w);
    if (mode == FOG_NONE) return lit;
    float d = distance(worldPos, eye);
    // f is transmittance: 1 = the surface is fully visible, 0 = fully fogged out.
    float f;
    if (mode == FOG_LINEAR) {
        f = clamp((fogParams.y - d) / max(fogParams.y - fogParams.x, 1e-4), 0.0, 1.0);
    } else {
        // exp(-(d*density)^2): Beer-Lambert with the exponent squared, which keeps
        // the foreground clear instead of hazing from the camera outward.
        float t = d * fogParams.z;
        f = exp(-t * t);
    }
    return mix(fogColor.rgb, lit, f);
}

#endif // PIX_LIGHTING_GLSL
