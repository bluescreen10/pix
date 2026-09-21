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
// Alias the same heap with a distinct shader variable for comparison sampling.
// SPIRV-Cross otherwise promotes every gTextures sample to depth2d's scalar
// return type, losing the G-buffer/base-color green and blue channels on Metal.
layout(set = 0, binding = 0) uniform texture2D gShadowTextures[];
layout(set = 0, binding = 2) uniform sampler gSamplers[];

const uint MAX_DIR = 4u;
const uint MAX_POINT = 16u;
const uint MAX_SPOT = 8u;
// Fog modes, mirroring pix.fogNone/fogLinear/fogExp2.
const uint FOG_NONE = 0u;
const uint FOG_LINEAR = 1u;
const uint FOG_EXP2 = 2u;

const uint NO_SHADOW = 0xFFFFFFFFu; // shadowMap sentinel: light casts no shadow

#define MAX_CASCADES 4
struct DirLight {
    vec4 dir;
    vec4 color;
    mat4 shadowVP[MAX_CASCADES];
    float shadowSplit[MAX_CASCADES];
    float shadowBias[MAX_CASCADES];
    float shadowTexel[MAX_CASCADES];
    float shadowDepthScale[MAX_CASCADES];
    uint shadowMap;
    uint cascades;
    // shadowMapSide is one cascade square's resolution in texels, which a wide kernel
    // needs to step in texels; shadowFilter is pix.ShadowFilter.
    uint shadowMapSide;
    uint shadowFilter;
};
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

// SHADOW_FILTER_* select how wide a kernel a shadow lookup uses. They match
// pix.ShadowFilter.
#define SHADOW_FILTER_HARD 0u
#define SHADOW_FILTER_SOFT 1u

// shadowTap is one hardware PCF fetch: the comparison and the bilinear blend of its four
// texels both happen in the texture unit, so a single tap already spans 2x2.
//
// uv is in the SHADOW CAMERA's own [0,1], not the texture's. Cascades share one texture
// laid out as `slots` squares along its width, so the remap here is what puts a cascade's
// own coordinates into its own column, and the clamp is what stops a wide kernel reading
// out of that column into its neighbour.
//
// A light with a map to itself passes slots = 1: the remap is then the identity and the
// clamp is skipped, because there is no neighbour to bleed into and shadowFactor has
// already rejected anything outside the map.
float shadowTap(uint shadowMap, uint shadowSamp, vec2 uv, float ref, uint slot, uint slots, uint mapSide) {
    if (slots > 1u) {
        float half_ = 0.5 / float(mapSide);
        uv = clamp(uv, vec2(half_), vec2(1.0 - half_));
        uv.x = (uv.x + float(slot)) / float(slots);
    }
    return texture(sampler2DShadow(gShadowTextures[nonuniformEXT(shadowMap)], gSamplers[nonuniformEXT(shadowSamp)]), vec3(uv, ref));
}

// shadowSoft is a 5x5 Gaussian approximation in nine hardware PCF taps.
//
// The trick, from Ignacio Castano's notes for The Witness, is that a bilinear PCF tap is
// already a weighted sum of four texels, and the weights are set by where in the texel
// the sample lands. So rather than take one tap per kernel entry, the taps are spread out
// so their 2x2 footprints tile the kernel without overlapping, and each one's sub-texel
// position and weight are solved for so the blend the texture unit performs reproduces
// the kernel's own weights. Nine taps then cover a 6x6 texel footprint exactly.
//
// The weights below are the separable kernel {1,3,4,3,1}, whose products give the 144 the
// sum is normalized by.
float shadowSoft(uint shadowMap, uint shadowSamp, vec2 uv, float ref, uint slot, uint slots, uint mapSide) {
    float side = float(mapSide);
    vec2 texels = uv * side;
    vec2 base = floor(texels + 0.5);
    float s = texels.x + 0.5 - base.x;
    float t = texels.y + 0.5 - base.y;
    vec2 baseUV = (base - 0.5) / side;

    float uw0 = 4.0 - 3.0 * s, uw1 = 7.0, uw2 = 1.0 + 3.0 * s;
    float u0 = (3.0 - 2.0 * s) / uw0 - 2.0;
    float u1 = (3.0 + s) / uw1;
    float u2 = s / uw2 + 2.0;

    float vw0 = 4.0 - 3.0 * t, vw1 = 7.0, vw2 = 1.0 + 3.0 * t;
    float v0 = (3.0 - 2.0 * t) / vw0 - 2.0;
    float v1 = (3.0 + t) / vw1;
    float v2 = t / vw2 + 2.0;

    float sum = 0.0;
    sum += uw0 * vw0 * shadowTap(shadowMap, shadowSamp, baseUV + vec2(u0, v0) / side, ref, slot, slots, mapSide);
    sum += uw1 * vw0 * shadowTap(shadowMap, shadowSamp, baseUV + vec2(u1, v0) / side, ref, slot, slots, mapSide);
    sum += uw2 * vw0 * shadowTap(shadowMap, shadowSamp, baseUV + vec2(u2, v0) / side, ref, slot, slots, mapSide);
    sum += uw0 * vw1 * shadowTap(shadowMap, shadowSamp, baseUV + vec2(u0, v1) / side, ref, slot, slots, mapSide);
    sum += uw1 * vw1 * shadowTap(shadowMap, shadowSamp, baseUV + vec2(u1, v1) / side, ref, slot, slots, mapSide);
    sum += uw2 * vw1 * shadowTap(shadowMap, shadowSamp, baseUV + vec2(u2, v1) / side, ref, slot, slots, mapSide);
    sum += uw0 * vw2 * shadowTap(shadowMap, shadowSamp, baseUV + vec2(u0, v2) / side, ref, slot, slots, mapSide);
    sum += uw1 * vw2 * shadowTap(shadowMap, shadowSamp, baseUV + vec2(u1, v2) / side, ref, slot, slots, mapSide);
    sum += uw2 * vw2 * shadowTap(shadowMap, shadowSamp, baseUV + vec2(u2, v2) / side, ref, slot, slots, mapSide);
    return sum * (1.0 / 144.0);
}

// shadowFactor returns the lit fraction (1 = fully lit, 0 = fully shadowed) for a light
// whose depth map is shadowVP/shadowMap.
//
// bias is in the shadow camera's NORMALIZED depth units. It cannot be a shared
// constant: a directional light's orthographic depth range spans the whole scene, so
// the same NDC value means a completely different world distance there than it does
// for a spot/point light whose range is local. Directional lights carry a bias
// computed from their fit; spot/point carry one derived from the light's range. Every
// light type carries its own, so LightShadow.Bias means something for all of them.
float shadowFactor(mat4 shadowVP, uint shadowMap, vec3 worldPos, uint shadowSamp, float bias,
                   uint slot, uint slots, uint mapSide, uint filterMode) {
    if (shadowMap == NO_SHADOW) return 1.0;
    vec4 c = shadowVP * vec4(worldPos, 1.0);
    if (c.w <= 0.0) return 1.0;
    vec3 ndc = c.xyz / c.w;
    vec2 uv = ndc.xy * 0.5 + 0.5;
    if (any(lessThan(uv, vec2(0.0))) || any(greaterThan(uv, vec2(1.0)))) {
        return 1.0;
    }
    // Reversed-Z: the far plane is 0 and nearer is GREATER, so "beyond the shadow
    // camera's far plane" is z < 0 rather than z > 1.
    if (ndc.z < 0.0) {
        return 1.0;
    }
    // Bias pushes the reference toward the light to avoid self-shadowing acne.
    // Under reversed-Z "toward the light" is a LARGER depth, so this adds where a
    // conventional depth buffer would subtract. The comparison sampler is
    // GreaterEqual to match (see Renderer.prepareShadows).
    float ref = ndc.z + bias;
    if (filterMode == SHADOW_FILTER_SOFT) {
        return shadowSoft(shadowMap, shadowSamp, uv, ref, slot, slots, mapSide);
    }
    return shadowTap(shadowMap, shadowSamp, uv, ref, slot, slots, mapSide);
}

// shadowOffsets returns how far to push a shadow lookup away from the surface, in
// shadow-map texels, given the surface normal and the direction to the light. x offsets
// along the normal, y along the light.
//
// Both scale with how obliquely the light meets the surface, and they are the whole
// reason a constant bias cannot work: a texel's footprint along the light stretches as
// 1/cos as the surface turns away, so the depth error inside one texel is unbounded
// while a constant is not. Under a low sun a bias big enough for the ground is many
// times what a wall needs, and sizing for the worst case detaches every shadow from its
// caster. The two scales are sin and tan of the angle between N and L respectively —
// the first is how far along the normal a texel's worth of surface travels, the second
// how much depth it covers. tan runs to infinity at grazing incidence, so it is capped.
//
// This is the formulation from Ignacio Castano's shadow mapping notes for The Witness.
const float shadowSlopeCap = 2.0;

vec2 shadowOffsets(vec3 N, vec3 L) {
    float cosAlpha = clamp(dot(N, L), 0.0, 1.0);
    float sinAlpha = sqrt(1.0 - cosAlpha * cosAlpha);
    return vec2(sinAlpha, min(shadowSlopeCap, sinAlpha / max(cosAlpha, 1e-3)));
}

// shadowNormalTexels and shadowSlopeTexels scale the two offsets above. They are in
// texels, so they hold across resolutions and cascades without retuning.
//
// The normal offset is kept small on purpose. It is the effective one against acne, but
// it moves the lookup rather than the comparison, so it slides the shadow across the
// surface as well as lifting it — too much and shadows visibly detach from their
// casters. Swept against a sun from overhead to near-grazing, three quarters of a texel
// removed as much acne as two did while giving back most of the shadow area that the
// larger offset ate.
const float shadowNormalTexels = 0.75;
const float shadowSlopeTexels = 2.0;

// sampleCascade applies cascade i's angle-dependent offsets and samples it.
float sampleCascade(DirLight dl, uint i, uint n, vec3 worldPos, vec3 N, uint shadowSamp) {
    float texel = dl.shadowTexel[i];
    vec2 off = shadowOffsets(N, -dl.dir.xyz);
    // Moving the lookup along the normal is what actually clears the surface; the slope
    // term covers the depth the texel still spans after that.
    vec3 p = worldPos + N * (texel * shadowNormalTexels * off.x);
    float bias = dl.shadowBias[i] + texel * shadowSlopeTexels * off.y * dl.shadowDepthScale[i];
    return shadowFactor(dl.shadowVP[i], dl.shadowMap, p, shadowSamp, bias,
                        i, n, dl.shadowMapSide, dl.shadowFilter);
}

// shadowCascadeBlend is how much of a cascade's depth range, at its far end, is shared
// with the next one out. Zero switches hard at the boundary.
const float shadowCascadeBlend = 0.1;

// dirShadowFactor picks which of a directional light's cascades covers this fragment and
// samples it. viewDist is how far the fragment is from the eye, which is what the split
// distances are measured in, and N is the surface normal, which sizes the offsets that
// keep the surface from shadowing itself.
//
// The cascades are ordered innermost first and the last one's split is the whole fitted
// range, so the first split that reaches past the fragment is the tightest map covering
// it. A light with one fit has one cascade whose split is that range, and the loop picks
// index 0 on its first test — the two paths are the same code, not a branch.
//
// Near a boundary both neighbours are sampled and crossfaded. Without that the switch is
// a visible seam, and not only because the two maps resolve the edge differently: the
// normal offset moves the lookup by a texel's worth of world space, and a texel is a
// different size in each cascade, so the shadow itself sits in a slightly different
// place either side of the line. Fading over the last tenth of the range spreads that
// step over enough pixels to disappear, at the cost of a second lookup for the fragments
// inside the band.
float dirShadowFactor(DirLight dl, vec3 worldPos, vec3 N, float viewDist, uint shadowSamp) {
    if (dl.shadowMap == NO_SHADOW) return 1.0;
    uint n = max(dl.cascades, 1u);
    uint i = n - 1u;
    for (uint c = 0u; c < n; c++) {
        if (viewDist <= dl.shadowSplit[c]) { i = c; break; }
    }

    float sh = sampleCascade(dl, i, n, worldPos, N, shadowSamp);
    if (i + 1u >= n) return sh; // the outermost cascade has nothing to fade into

    // How far into this cascade's fade band the fragment sits. The band is measured back
    // from the split, over a fraction of the range this cascade spans.
    float near = (i == 0u) ? 0.0 : dl.shadowSplit[i - 1u];
    float band = (dl.shadowSplit[i] - near) * shadowCascadeBlend;
    if (band <= 0.0) return sh;
    float t = clamp((viewDist - (dl.shadowSplit[i] - band)) / band, 0.0, 1.0);
    if (t <= 0.0) return sh;

    return mix(sh, sampleCascade(dl, i + 1u, n, worldPos, N, shadowSamp), t);
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
    return shadowFactor(pl.shadowVP[face], pl.shadowMap[face], worldPos, shadowSamp, pl.shadowBias,
                        0u, 1u, 1u, SHADOW_FILTER_HARD);
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
// qualifier, the same reason shadeSurface reads the table from the push constants directly.
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
