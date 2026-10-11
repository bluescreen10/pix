#version 460
#extension GL_EXT_buffer_reference : require
#extension GL_EXT_buffer_reference2 : require
#extension GL_EXT_scalar_block_layout : require
#extension GL_EXT_nonuniform_qualifier : require
#extension GL_EXT_shader_explicit_arithmetic_types_int64 : require
#extension GL_GOOGLE_include_directive : require

// BlinnPhongMaterial: ambient + per-light diffuse (N·L) + Blinn-Phong specular.
#include <pix/material_common.glsl>

layout(location = 0) out vec4 outColor;

// Material mirrors materials.BlinnPhongMaterial's record (96 bytes).
struct Material {
    vec4 color;
    vec3 emissive;
    float shininess;
    vec3 specular;
    uint colorMap;
    uint samp;
    uint flags;
    uint uv2Maps;
    uint pad0;    UVTransform uv1Transform;
    UVTransform uv2Transform;
};
layout(buffer_reference, scalar) readonly buffer MatBuf { Material v[]; };

const float PI = 3.14159265359;

// blinnPhong returns the radiance one light, arriving from L with radiance, leaves the
// surface with toward the eye: Lambert diffuse and a normalized Blinn-Phong highlight,
// both scaled by N·L. The highlight's (shininess / 2 + 1) / PI normalization keeps its
// energy as it narrows, so a shinier surface gets a smaller, brighter highlight rather
// than a smaller, dimmer one; Schlick's Fresnel on the specular colour brightens it at
// grazing angles. Diffuse divides by PI, as the PBR material's does, so the same light
// lights both material classes alike.
vec3 blinnPhong(vec3 N, vec3 V, vec3 L, vec3 radiance, vec3 albedo, vec3 specularColor, float shininess) {
    float NdotL = max(dot(N, L), 0.0);
    vec3 H = normalize(L + V);
    float NdotH = max(dot(N, H), 0.0);
    float VdotH = max(dot(V, H), 0.0);

    float x = 1.0 - VdotH;
    float x2 = x * x;
    vec3 fresnel = specularColor + (1.0 - specularColor) * (x2 * x2 * x);
    // Blinn-Phong's implicit visibility term is the constant 1/4.
    float distribution = (shininess * 0.5 + 1.0) / PI * pow(NdotH, shininess);
    vec3 specular = fresnel * (0.25 * distribution);

    vec3 diffuse = albedo / PI;
    return radiance * NdotL * (diffuse + specular);
}

void main() {
    discardCutOut();
    Material m = MatBuf(pc.materials).v[vMat];
    MaterialUVs uvs = materialUVs(m.uv1Transform, m.uv2Transform);
    vec4 base = sampleBase(m.color, m.flags, m.colorMap, m.samp, mapUV(uvs, m.uv2Maps, MAT_COLOR_MAP));
    vec3 albedo = base.rgb;

    LightBuf L = pc.lights;
    // A back face is drawn only for a double-sided surface, which is lit on the side
    // it is seen from.
    vec3 N = gl_FrontFacing ? normalize(vNormal) : -normalize(vNormal);
    vec3 toEye = pc.eye.xyz - vWorldPos;
    float viewDist = length(toEye);
    vec3 V = toEye / viewDist;

    uint shadowSamp = pc.shadowSampler;
    bool receives = (vFlags & FLAG_RECEIVES_SHADOW) != 0u;

    // The environment's diffuse light where the scene has one, else the ambient colour.
    vec3 ambient = L.ambient.rgb;
    if (hasEnvironment(L)) {
        ambient = environmentDiffuse(L, N);
    }
    vec3 indirect = ambient * albedo;
    vec3 lit = indirect;
    for (uint i = 0u; i < L.numDir; i++) {
        vec3 Ldir = normalize(-L.dirs[i].dir.xyz);
        // Nothing reaches a surface turned away from the light, so neither the shading
        // nor the shadow lookup that would scale it is worth paying for.
        if (dot(N, Ldir) <= 0.0) continue;
        float sh = receives ? dirShadowFactor(L, i, vWorldPos, N, shadowSamp) : 1.0;
        vec3 radiance = L.dirs[i].color.rgb * L.dirs[i].color.w * dirMask(L, i, vWorldPos);
        lit += sh * blinnPhong(N, V, Ldir, radiance, albedo, m.specular, m.shininess);
    }
    // Point and spot lights: only those the fragment's cluster lists.
    uint cluster = lightCluster(L, vWorldPos);
    uint pointCount = clusterPointCount(L, cluster);
    uint spotCount = clusterSpotCount(L, cluster);
    for (uint i = 0u; i < pointCount; i++) {
        uint li = clusterLight(L, cluster, i);
        PointLight pl = L.points.v[li];
        vec3 d = pl.pos.xyz - vWorldPos;
        float dist = length(d);
        float atten = pointAttenuation(pl, dist);
        vec3 Ldir = d / max(dist, 0.0001);
        if (atten <= 0.0 || dot(N, Ldir) <= 0.0) continue;
        float sh = receives ? pointShadowFactor(L, li, vWorldPos, shadowSamp) : 1.0;
        lit += sh * blinnPhong(N, V, Ldir, pl.color.rgb * pl.color.w * atten, albedo, m.specular, m.shininess);
    }
    for (uint i = 0u; i < spotCount; i++) {
        uint li = clusterLight(L, cluster, pointCount + i);
        SpotLight sl = L.spots.v[li];
        vec3 d = sl.pos.xyz - vWorldPos;
        float dist = length(d);
        vec3 Ldir = d / max(dist, 1e-4);
        float atten = spotAttenuation(sl, vWorldPos, Ldir, dist);
        if (atten <= 0.0 || dot(N, Ldir) <= 0.0) continue;
        float sh = receives ? spotShadowFactor(L, li, vWorldPos, shadowSamp) : 1.0;
        lit += sh * blinnPhong(N, V, Ldir, sl.color.rgb * sl.color.w * atten, albedo, m.specular, m.shininess);
    }

    lit += m.emissive.rgb;
    Fog fog = fogAt(viewDist, L.fogColor, L.fogParams);
    vec3 color = applyFog(lit, fog);
    outColor = vec4(color, outputAlpha(color, indirect, fog, base.a)); // linear: the target encodes it for display
}
