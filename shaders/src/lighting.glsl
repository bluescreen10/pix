// lighting.glsl — the light table, shadow sampling and fog, shared by every lit shader.
// Everything here works in linear light; the render target encodes for display.
#ifndef PIX_LIGHTING_GLSL
#define PIX_LIGHTING_GLSL

#include "bindless.glsl"

const uint MAX_DIR = 4u;
const uint MAX_POINT = 16u;
const uint MAX_SPOT = 8u;
// Fog modes, mirroring scenes.FogNone/FogLinear/FogExp2/FogVolumetric.
const uint FOG_NONE = 0u;
const uint FOG_LINEAR = 1u;
const uint FOG_EXP2 = 2u;
const uint FOG_VOLUMETRIC = 3u;

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

// shadowSlot is the transform from a shadow camera's own [0,1] into the texture it
// shares with the other cascades: x scaled into this cascade's column, and the bounds a
// wide kernel has to stay inside so it cannot read its neighbour's texels.
//
// Cascades lay their squares out along the width, so a cascade's coordinates are its
// column's coordinates once scaled and offset. A light with a map to itself passes
// slots = 1, which makes the scale the identity and the clamp the full map.
struct ShadowSlot {
    float scale;  // 1 / slots
    float offset; // slot / slots
    vec2 lo, hi;  // the column's usable range, half a texel in from its edges
};

ShadowSlot shadowSlot(uint slot, uint slots, uint mapSide) {
    float inv = 1.0 / float(slots);
    ShadowSlot s;
    s.scale = inv;
    s.offset = float(slot) * inv;
    // A light with the map to itself has no neighbour to bleed into, and shadowFactor
    // has already rejected anything off the map, so the clamp is the full range. It
    // stays in the expression rather than behind a branch because it costs nothing and
    // those lights do not publish a map resolution to inset by.
    s.lo = vec2(0.0);
    s.hi = vec2(1.0);
    if (slots > 1u) {
        float half_ = 0.5 / float(mapSide);
        s.lo = vec2(half_);
        s.hi = vec2(1.0 - half_);
    }
    return s;
}

// shadowTap is one hardware PCF fetch: the comparison and the bilinear blend of its four
// texels both happen in the texture unit, so a single tap already spans 2x2. It names
// its level, the map's only one, rather than letting derivatives pick it: compute
// shaders, which have none, sample shadows too (see fog_inject.comp.glsl).
//
// uv is in the SHADOW CAMERA's own [0,1], not the texture's; the slot maps it the rest
// of the way. Taking that as a precomputed value rather than deriving it per tap matters
// when nine of these run per light per fragment.
float shadowTap(uint shadowMap, uint shadowSamp, vec2 uv, float ref, ShadowSlot s) {
    uv = clamp(uv, s.lo, s.hi);
    uv.x = uv.x * s.scale + s.offset;
    return textureLod(sampler2DShadow(gShadowTextures[nonuniformEXT(shadowMap)], gSamplers[nonuniformEXT(shadowSamp)]), vec3(uv, ref), 0.0);
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
float shadowSoft(uint shadowMap, uint shadowSamp, vec2 uv, float ref, ShadowSlot slot, float side) {
    vec2 texels = uv * side;
    vec2 base = floor(texels + 0.5);
    float s = texels.x + 0.5 - base.x;
    float t = texels.y + 0.5 - base.y;
    vec2 baseUV = (base - 0.5) / side;
    float inv = 1.0 / side;

    float uw0 = 4.0 - 3.0 * s, uw1 = 7.0, uw2 = 1.0 + 3.0 * s;
    float u0 = ((3.0 - 2.0 * s) / uw0 - 2.0) * inv;
    float u1 = ((3.0 + s) / uw1) * inv;
    float u2 = (s / uw2 + 2.0) * inv;

    float vw0 = 4.0 - 3.0 * t, vw1 = 7.0, vw2 = 1.0 + 3.0 * t;
    float v0 = ((3.0 - 2.0 * t) / vw0 - 2.0) * inv;
    float v1 = ((3.0 + t) / vw1) * inv;
    float v2 = (t / vw2 + 2.0) * inv;

    float sum = 0.0;
    sum += uw0 * vw0 * shadowTap(shadowMap, shadowSamp, baseUV + vec2(u0, v0), ref, slot);
    sum += uw1 * vw0 * shadowTap(shadowMap, shadowSamp, baseUV + vec2(u1, v0), ref, slot);
    sum += uw2 * vw0 * shadowTap(shadowMap, shadowSamp, baseUV + vec2(u2, v0), ref, slot);
    sum += uw0 * vw1 * shadowTap(shadowMap, shadowSamp, baseUV + vec2(u0, v1), ref, slot);
    sum += uw1 * vw1 * shadowTap(shadowMap, shadowSamp, baseUV + vec2(u1, v1), ref, slot);
    sum += uw2 * vw1 * shadowTap(shadowMap, shadowSamp, baseUV + vec2(u2, v1), ref, slot);
    sum += uw0 * vw2 * shadowTap(shadowMap, shadowSamp, baseUV + vec2(u0, v2), ref, slot);
    sum += uw1 * vw2 * shadowTap(shadowMap, shadowSamp, baseUV + vec2(u1, v2), ref, slot);
    sum += uw2 * vw2 * shadowTap(shadowMap, shadowSamp, baseUV + vec2(u2, v2), ref, slot);
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
    ShadowSlot s = shadowSlot(slot, slots, mapSide);
    if (filterMode == SHADOW_FILTER_SOFT) {
        return shadowSoft(shadowMap, shadowSamp, uv, ref, s, float(mapSide));
    }
    return shadowTap(shadowMap, shadowSamp, uv, ref, s);
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

// sampleCascade applies cascade i's angle-dependent offsets and samples it. off is
// shadowOffsets for this fragment, passed in because it depends only on the surface and
// the light — recomputing its square root per cascade would pay twice for the same
// answer on every fragment inside a blend band.
float sampleCascade(LightBuf L, uint li, uint i, uint n, vec3 worldPos, vec3 N, vec2 off, uint shadowSamp) {
    float texel = L.dirs[li].shadowTexel[i];
    // Moving the lookup along the normal is what actually clears the surface; the slope
    // term covers the depth the texel still spans after that.
    vec3 p = worldPos + N * (texel * shadowNormalTexels * off.x);
    float bias = L.dirs[li].shadowBias[i] + texel * shadowSlopeTexels * off.y * L.dirs[li].shadowDepthScale[i];
    return shadowFactor(L.dirs[li].shadowVP[i], L.dirs[li].shadowMap, p, shadowSamp, bias,
                        i, n, L.dirs[li].shadowMapSide, L.dirs[li].shadowFilter);
}

// shadowCascadeBlend is how much of a cascade's depth range, at its far end, is shared
// with the next one out. Zero switches hard at the boundary.
const float shadowCascadeBlend = 0.1;

// dirShadowFactor picks which of a directional light's cascades covers this fragment and
// samples it. It takes the light's INDEX rather than the light, and so do the helpers it
// calls: a DirLight carries a matrix per cascade and runs to a few hundred bytes, so
// copying one into a local — which is what naming it as a parameter or assigning it to a
// variable does — costs more register traffic than the lookup it is there to perform.
// Reading the two or three fields actually wanted straight out of the buffer is free by
// comparison. The buffer itself is passed as a reference, which is a 64-bit handle. viewDist is how far the fragment is from the eye, which is what the split
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
float dirShadowFactor(LightBuf L, uint li, vec3 worldPos, vec3 N, float viewDist, uint shadowSamp) {
    if (L.dirs[li].shadowMap == NO_SHADOW) return 1.0;
    uint n = max(L.dirs[li].cascades, 1u);
    uint i = n - 1u;
    for (uint c = 0u; c < n; c++) {
        if (viewDist <= L.dirs[li].shadowSplit[c]) { i = c; break; }
    }

    vec2 off = shadowOffsets(N, -L.dirs[li].dir.xyz);
    float sh = sampleCascade(L, li, i, n, worldPos, N, off, shadowSamp);
    if (i + 1u >= n) return sh; // the outermost cascade has nothing to fade into

    // How far into this cascade's fade band the fragment sits. The band is measured back
    // from the split, over a fraction of the range this cascade spans.
    float near = (i == 0u) ? 0.0 : L.dirs[li].shadowSplit[i - 1u];
    float band = (L.dirs[li].shadowSplit[i] - near) * shadowCascadeBlend;
    if (band <= 0.0) return sh;
    float t = clamp((viewDist - (L.dirs[li].shadowSplit[i] - band)) / band, 0.0, 1.0);
    if (t <= 0.0) return sh;

    return mix(sh, sampleCascade(L, li, i + 1u, n, worldPos, N, off, shadowSamp), t);
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

// fogVolumeSlice is the depth coordinate in the volumetric fog volume for a point d
// world units from the eye. Slices are spaced by the square root of the distance, so
// they crowd near the camera, where a slice covers the most screen; and the volume
// holds, at each slice's centre, the fog up to that slice's far edge, which a lookup
// half a slice back lands on. slices is the volume's depth in slices, and reach how far
// from the eye it extends.
float fogVolumeSlice(float d, float reach, float slices) {
    return sqrt(clamp(d / reach, 0.0, 1.0)) - 0.5 / slices;
}

// applyFog reads gl_FragCoord, which only a fragment shader has; a compute shader that
// includes this file for its light table defines PIX_NO_FRAGMENT_FOG to leave it out.
#ifndef PIX_NO_FRAGMENT_FOG
// applyFog blends a LINEAR-space shaded colour toward the scene's fog colour by
// distance from the eye. Fog is a physical blend between the surface and the medium
// in front of it, so it belongs in linear light, before anything encodes for display.
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
    if (mode == FOG_VOLUMETRIC) {
        // The renderer has already worked out, for every froxel, what the fog between
        // it and the eye adds and how much it lets through (see fog_integrate.comp);
        // all that is left is to look it up. fogColor is (1/width, 1/height of the
        // screen the volume lies over, the fog's reach, mode), fogParams (volume,
        // sampler, slices, _) — see pix.Lights.rebuild.
        vec3 uvw = vec3(gl_FragCoord.xy * fogColor.xy, fogVolumeSlice(d, fogColor.z, fogParams.z));
        vec4 fog = textureLod(sampler3D(gTextures3D[nonuniformEXT(uint(fogParams.x))], gSamplers[nonuniformEXT(uint(fogParams.y))]), uvw, 0.0);
        return lit * fog.a + fog.rgb;
    }
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
#endif // PIX_NO_FRAGMENT_FOG

#endif // PIX_LIGHTING_GLSL
