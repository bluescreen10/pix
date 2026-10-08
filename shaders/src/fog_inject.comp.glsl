#version 460
#extension GL_EXT_buffer_reference : require
#extension GL_EXT_buffer_reference2 : require
#extension GL_EXT_scalar_block_layout : require
#extension GL_EXT_nonuniform_qualifier : require
#extension GL_EXT_shader_explicit_arithmetic_types_int64 : require
#extension GL_GOOGLE_include_directive : require

// fog_inject — the first of volumetric fog's two passes. For every froxel of the volume
// laid over the camera's view it works out how dense the fog is there, and how much
// light the fog there sends toward the camera: ambient light, its own emission, and
// every directional, point and spot light, each through its shadow map — and a
// directional light through its mask too. It writes that,
// per unit of distance, with the density, into the medium volume, which fog_integrate
// then accumulates along each column.
#define PIX_NO_FRAGMENT_FOG
#include "lighting.glsl"

layout(local_size_x = 4, local_size_y = 4, local_size_z = 4) in;

// Matches fogInjectRoot in volumetric_fog.go.
layout(push_constant, scalar) uniform PC {
    mat4 inverseViewProj;
    LightBuf lights;
    vec3 eye;
    float density;
    vec3 albedo;
    float anisotropy;
    vec3 emission;
    float baseHeight;
    float heightFalloff;
    float reach;
    uint medium;
    uint shadowSampler;
    uvec3 size;
    uint pad0;
    uint pad1;
    uint pad2;
} pc;

// phase is the Henyey-Greenstein phase function: the share of the light arriving along
// one direction that the fog scatters off at an angle with cosine cosTheta from it, for
// anisotropy g. It is scaled by pi, as the lit shaders' Lambert term is — a light's
// intensity is what a white surface facing it reflects in full — so fog under a light
// is as bright as the surfaces beside it.
float phase(float cosTheta, float g) {
    float g2 = g * g;
    return (1.0 - g2) / (4.0 * pow(max(1.0 + g2 - 2.0 * g * cosTheta, 1e-4), 1.5));
}

// dirShadow is how much of directional light li reaches p: the cascade covering p's view
// depth (see dirShadowFactor), without the offsets a surface needs to keep from
// shadowing itself — fog has no surface.
float dirShadow(LightBuf L, uint li, vec3 p, uint shadowSamp) {
    if (L.dirs[li].shadowMap == NO_SHADOW) {
        return 1.0;
    }
    float depth = viewDepth(L, p);
    uint n = max(L.dirs[li].cascades, 1u);
    uint i = n - 1u;
    for (uint c = 0u; c < n; c++) {
        if (depth <= L.dirs[li].shadowSplit[c]) {
            i = c;
            break;
        }
    }
    return shadowFactor(L.dirs[li].shadowVP[i], L.dirs[li].shadowMap, p, shadowSamp, L.dirs[li].shadowBias[i],
                        i, n, L.dirs[li].shadowMapSide, SHADOW_FILTER_HARD);
}

void main() {
    uvec3 id = gl_GlobalInvocationID;
    if (any(greaterThanEqual(id, pc.size))) {
        return;
    }

    // The froxel's centre: on its column's ray from the camera, at the distance of its
    // slice's centre (fogVolumeSlice in lighting.glsl spaces the slices).
    vec3 cell = (vec3(id) + 0.5) / vec3(pc.size);
    vec2 ndc = cell.xy * 2.0 - 1.0;
    vec4 nearPoint = pc.inverseViewProj * vec4(ndc, 1.0, 1.0); // reversed depth: 1 is the near plane
    vec4 midPoint = pc.inverseViewProj * vec4(ndc, 0.5, 1.0);
    vec3 ray = normalize(midPoint.xyz / midPoint.w - nearPoint.xyz / nearPoint.w);
    float d = cell.z * cell.z * pc.reach;
    vec3 p = pc.eye + ray * d;

    float density = pc.density * exp(-pc.heightFalloff * max(p.y - pc.baseHeight, 0.0));

    // Light scattered toward the camera leaves along -ray; cosTheta is between that and
    // the direction the light was travelling.
    LightBuf L = pc.lights;
    vec3 inscattered = L.ambient.rgb;
    for (uint i = 0u; i < L.numDir; i++) {
        vec3 travel = normalize(L.dirs[i].dir.xyz);
        vec3 radiance = L.dirs[i].color.rgb * L.dirs[i].color.w * dirMask(L, i, p);
        inscattered += radiance * phase(dot(travel, -ray), pc.anisotropy) * dirShadow(L, i, p, pc.shadowSampler);
    }
    // Point and spot lights: only those the froxel's light cluster lists. The cluster
    // grid and the fog volume both lie over the main view, so a froxel finds its cell as
    // a surface does.
    uint cluster = lightCluster(L, p);
    uint pointCount = clusterPointCount(L, cluster);
    uint spotCount = clusterSpotCount(L, cluster);
    for (uint i = 0u; i < pointCount; i++) {
        uint li = clusterLight(L, cluster, i);
        PointLight pl = L.points.v[li];
        vec3 fromLight = p - pl.pos.xyz;
        float dist = length(fromLight);
        float atten = pointAttenuation(pl, dist);
        if (atten <= 0.0) {
            continue;
        }
        vec3 travel = fromLight / max(dist, 1e-4);
        vec3 radiance = pl.color.rgb * pl.color.w * atten;
        inscattered += radiance * phase(dot(travel, -ray), pc.anisotropy) * pointShadowFactor(L, li, p, pc.shadowSampler);
    }
    for (uint i = 0u; i < spotCount; i++) {
        uint li = clusterLight(L, cluster, pointCount + i);
        SpotLight sl = L.spots.v[li];
        vec3 toLight = sl.pos.xyz - p;
        float dist = length(toLight);
        vec3 Ldir = toLight / max(dist, 1e-4);
        float atten = spotAttenuation(sl, p, Ldir, dist);
        if (atten <= 0.0) {
            continue;
        }
        vec3 radiance = sl.color.rgb * sl.color.w * atten;
        float sh = spotShadowFactor(L, li, p, pc.shadowSampler);
        inscattered += radiance * phase(dot(-Ldir, -ray), pc.anisotropy) * sh;
    }

    vec3 emitted = density * (pc.albedo * inscattered + pc.emission);
    imageStore(gImages3D[nonuniformEXT(pc.medium)], ivec3(id), vec4(emitted, density));
}
