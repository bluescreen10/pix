// lighting.glsl — the light table, shadow sampling and fog, shared by every lit shader.
// Everything here works in linear light; the render target encodes for display.
#ifndef PIX_LIGHTING_GLSL
#define PIX_LIGHTING_GLSL

#include "bindless.glsl"
#include "environment.glsl"

const uint MAX_DIR = 4u;
// Fog modes, mirroring scenes.FogNone/FogLinear/FogExp2/FogVolumetric.
const uint FOG_NONE = 0u;
const uint FOG_LINEAR = 1u;
const uint FOG_EXP2 = 2u;
const uint FOG_VOLUMETRIC = 3u;

const uint NO_SHADOW = 0xFFFFFFFFu; // shadowMap sentinel: light casts no shadow
const uint NO_MASK = 0xFFFFFFFFu;   // mask sentinel: directional light has no mask
const uint NO_ENVIRONMENT = 0xFFFFFFFFu; // envRadiance sentinel: the scene has no environment

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
    // maskU and maskV take a world position to the light's mask coordinates, and mask
    // is its heap index, or NO_MASK (see dirMask and pix.maskProjection).
    vec4 maskU;
    vec4 maskV;
    uint mask;
    uint maskSampler;
};
// Point and spot lights are unbounded lists, each light a small record a shading loop
// reads in full; the few that cast shadows point at their shadow's own record, which
// is too large to be worth reading for the many that do not.
struct PointLight {
    vec4 pos;   // xyz world; w = range
    vec4 color; // rgb; w = intensity
    uint shadow; // index into LightBuf.pointShadows, or NO_SHADOW
    uint pad0;
    uint pad1;
    uint pad2;
};
struct PointShadow { mat4 shadowVP[6]; uint shadowMap[6]; float shadowBias; uint pad0; };
struct SpotLight {
    vec4 pos;   // xyz world; w = range
    vec4 dir;   // xyz cone axis (travel); w = cos of the outer cutoff
    vec4 color; // rgb; w = intensity
    float cosInner;
    uint shadow; // index into LightBuf.spotShadows, or NO_SHADOW
    uint pad0;
    uint pad1;
};
struct SpotShadow { mat4 shadowVP; uint shadowMap; float shadowBias; uint pad0; uint pad1; };
layout(buffer_reference, scalar) readonly buffer PointLights { PointLight v[]; };
layout(buffer_reference, scalar) readonly buffer PointShadows { PointShadow v[]; };
layout(buffer_reference, scalar) readonly buffer SpotLights { SpotLight v[]; };
layout(buffer_reference, scalar) readonly buffer SpotShadows { SpotShadow v[]; };

// Light clusters: the main view is cut into CLUSTER_X x CLUSTER_Y tiles across the
// screen and CLUSTER_Z slices in depth, and each of those cells lists the point and
// spot lights whose range reaches into it (see light_clusters.comp.glsl). A surface
// shades only with its own cell's lights, so the cost of a light is paid only where it
// lands. They mirror pix's clusterX, clusterY, clusterZ and clusterCapacity.
//
// A cell is CLUSTER_STRIDE uints: a count of its point lights in the low 16 bits and of
// its spot lights in the high 16, then the point lights' indices, then the spot
// lights'. A cell lists at most CLUSTER_CAPACITY lights, and drops the rest.
const uint CLUSTER_X = 16u;
const uint CLUSTER_Y = 9u;
const uint CLUSTER_Z = 24u;
const uint CLUSTER_CAPACITY = 255u;
const uint CLUSTER_STRIDE = CLUSTER_CAPACITY + 1u;
layout(buffer_reference, scalar) readonly buffer LightClusters { uint v[]; };

layout(buffer_reference, scalar) readonly buffer LightBuf {
    vec4 ambient;
    vec4 fogColor;  // rgb = fog colour, w = FOG_* mode
    vec4 fogParams; // (near, far, density, _)
    uint numDir;
    uint numPoint;
    uint numSpot;
    uint pad0;
    // The environment the scene is lit by (see pix.environmentState): its prefiltered
    // reflections — one mip per roughness, the roughest also its diffuse light — and the
    // BRDF table reflections are weighted by; envRadiance is NO_ENVIRONMENT when there
    // is none.
    uint envRadiance;
    uint envSampler;
    uint envMips;
    uint envBRDF;
    float envIntensity;
    float envRotation;
    PointLights points;
    SpotLights spots;
    PointShadows pointShadows;
    SpotShadows spotShadows;
    // clusters is the main view's cells; clusterViewProj and clusterDepth find the cell
    // a world position falls in — its tile through the view's projection, its slice
    // from its depth along the view, sliced as log(depth) * clusterSliceScale +
    // clusterSliceBias (see lightCluster).
    LightClusters clusters;
    mat4 clusterViewProj;
    vec4 clusterDepth;
    float clusterSliceScale;
    float clusterSliceBias;
    DirLight dirs[MAX_DIR];
};

// lightCluster returns the cluster worldPos falls in: where its cell starts in
// LightBuf.clusters. Positions off the
// view, nearer than its near plane or beyond its far plane, are clamped to the nearest
// cell, which is what lets a compute pass marching past the far plane look lights up
// too.
uint lightCluster(LightBuf L, vec3 worldPos) {
    vec4 clip = L.clusterViewProj * vec4(worldPos, 1.0);
    vec2 uv = clip.xy / max(clip.w, 1e-6) * 0.5 + 0.5;
    uvec2 tile = uvec2(clamp(uv, vec2(0.0), vec2(1.0)) * vec2(CLUSTER_X, CLUSTER_Y));
    tile = min(tile, uvec2(CLUSTER_X - 1u, CLUSTER_Y - 1u));
    float depth = dot(L.clusterDepth.xyz, worldPos) + L.clusterDepth.w;
    float slice = log(max(depth, 1e-6)) * L.clusterSliceScale + L.clusterSliceBias;
    uint z = uint(clamp(slice, 0.0, float(CLUSTER_Z - 1u)));
    return ((z * CLUSTER_Y + tile.y) * CLUSTER_X + tile.x) * CLUSTER_STRIDE;
}

// clusterPointCount and clusterSpotCount read how many lights of each kind a cluster
// lists; clusterLight reads its i-th light's index, counting its point lights first.
uint clusterPointCount(LightBuf L, uint cluster) {
    return L.clusters.v[cluster] & 0xFFFFu;
}

uint clusterSpotCount(LightBuf L, uint cluster) {
    return L.clusters.v[cluster] >> 16;
}

uint clusterLight(LightBuf L, uint cluster, uint i) {
    return L.clusters.v[cluster + 1u + i];
}

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

// dirMask is how much of directional light li gets through its mask at worldPos — the
// mask's red channel where the light's ray through worldPos crosses it, or 1 for a light
// without one. Clouds far above shade a point this way (see scenes.LightMask). It reads
// the mask's base level by name, so a compute shader reads it as a fragment shader does.
float dirMask(LightBuf L, uint li, vec3 worldPos) {
    if (L.dirs[li].mask == NO_MASK) {
        return 1.0;
    }
    vec2 uv = vec2(dot(L.dirs[li].maskU.xyz, worldPos) + L.dirs[li].maskU.w,
                   dot(L.dirs[li].maskV.xyz, worldPos) + L.dirs[li].maskV.w);
    return textureLod(sampler2D(gTextures[nonuniformEXT(L.dirs[li].mask)], gSamplers[nonuniformEXT(L.dirs[li].maskSampler)]), uv, 0.0).r;
}

// environmentFrame turns a world direction into the environment's own, undoing its
// rotation about the vertical.
vec3 environmentFrame(LightBuf L, vec3 dir) {
    return unrotateEnvironment(dir, L.envRotation);
}

// hasEnvironment reports whether the scene is lit by an environment rather than by its
// flat ambient colour.
bool hasEnvironment(LightBuf L) {
    return L.envRadiance != NO_ENVIRONMENT;
}

// environmentDiffuse is the light a white diffuse surface facing N takes from the
// environment: the same kind of value as the ambient colour it replaces, so a uniform
// white environment lights such a surface to exactly 1. It is the roughest mip of the
// prefiltered reflections, looked up along the normal for a sky's diffuse light too. 
// That mip is the environment blurred by a GGX lobe of roughness 1, a little narrower
// than the cosine lobe diffuse light is, which is close enough for light that changes 
// this slowly across normals.
vec3 environmentDiffuse(LightBuf L, vec3 N) {
    vec2 uv = equirectUV(environmentFrame(L, N));
    return textureLod(sampler2D(gTextures[nonuniformEXT(L.envRadiance)], gSamplers[nonuniformEXT(L.envSampler)]), uv, float(L.envMips - 1u)).rgb * L.envIntensity;
}

// environmentSpecular is the environment as a surface of the given roughness mirrors it
// along R: the prefiltered image, at the mip blurred for that roughness.
vec3 environmentSpecular(LightBuf L, vec3 R, float roughness) {
    vec2 uv = equirectUV(environmentFrame(L, R));
    float lod = roughness * float(L.envMips - 1u);
    return textureLod(sampler2D(gTextures[nonuniformEXT(L.envRadiance)], gSamplers[nonuniformEXT(L.envSampler)]), uv, lod).rgb * L.envIntensity;
}

// environmentBRDF is the split-sum pair (scale, bias) a reflection of the environment
// is weighted by: it reflects f0 * scale + bias of what environmentSpecular returns.
// The table is read through the environment's sampler, which wraps across, so the
// lookup stays half a texel inside the table's edges: at NdotV = 1 it would otherwise
// blend in the NdotV = 0 column.
vec2 environmentBRDF(LightBuf L, float NdotV, float roughness) {
    vec2 uv = clamp(vec2(NdotV, roughness), vec2(0.5 / ENV_BRDF_SIZE), vec2(1.0 - 0.5 / ENV_BRDF_SIZE));
    return textureLod(sampler2D(gTextures[nonuniformEXT(L.envBRDF)], gSamplers[nonuniformEXT(L.envSampler)]), uv, 0.0).rg;
}

// pointShadowFactor picks the cube face for the light→fragment direction (dominant
// axis, matching pix's cubeFaceDirs order +X,-X,+Y,-Y,+Z,-Z) and samples that face of
// point light li's shadow.
float pointShadowFactor(LightBuf L, uint li, vec3 worldPos, uint shadowSamp) {
    uint si = L.points.v[li].shadow;
    if (si == NO_SHADOW) return 1.0;
    vec3 v = worldPos - L.points.v[li].pos.xyz;
    vec3 a = abs(v);
    uint face;
    if (a.x >= a.y && a.x >= a.z) {
        face = v.x > 0.0 ? 0u : 1u;
    } else if (a.y >= a.z) {
        face = v.y > 0.0 ? 2u : 3u;
    } else {
        face = v.z > 0.0 ? 4u : 5u;
    }
    return shadowFactor(L.pointShadows.v[si].shadowVP[face], L.pointShadows.v[si].shadowMap[face], worldPos, shadowSamp,
                        L.pointShadows.v[si].shadowBias, 0u, 1u, 1u, SHADOW_FILTER_HARD);
}

// spotShadowFactor samples spot light li's shadow.
float spotShadowFactor(LightBuf L, uint li, vec3 worldPos, uint shadowSamp) {
    uint si = L.spots.v[li].shadow;
    if (si == NO_SHADOW) return 1.0;
    return shadowFactor(L.spotShadows.v[si].shadowVP, L.spotShadows.v[si].shadowMap, worldPos, shadowSamp,
                        L.spotShadows.v[si].shadowBias, 0u, 1u, 1u, SHADOW_FILTER_HARD);
}

// pointAttenuation is a point light's distance falloff: 1 at the light, 0 at its range.
float pointAttenuation(PointLight pl, float dist) {
    float atten = clamp(1.0 - dist / max(pl.pos.w, 1e-4), 0.0, 1.0);
    return atten * atten;
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
