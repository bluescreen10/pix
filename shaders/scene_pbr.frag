#version 460
#extension GL_EXT_buffer_reference : require
#extension GL_EXT_buffer_reference2 : require
#extension GL_EXT_scalar_block_layout : require
#extension GL_EXT_nonuniform_qualifier : require
#extension GL_EXT_shader_explicit_arithmetic_types_int64 : require
#extension GL_GOOGLE_include_directive : require

// PBRMaterial: metallic-roughness Cook-Torrance (GGX, Smith, Schlick), flat ambient.
#include "material_common.glsl"

// Material mirrors pix.pbrRecord (80 bytes). Each map carries its own sampler.
struct Material {
    vec4 color;
    vec4 emissive;
    float metallic;
    float roughness;
    float transmission;
    uint flags;
    uint colorMap;
    uint colorSampler;
    uint normalMap;
    uint normalSampler;
    uint metalMap;
    uint metalSampler;
    uint roughMap;
    uint roughSampler;
};
layout(buffer_reference, scalar) readonly buffer MatBuf { Material v[]; };

const uint MAT_NORMAL_MAP = 2u;
const uint MAT_METAL_MAP = 4u;
const uint MAT_ROUGH_MAP = 8u;
const float PI = 3.14159265359;

// tex samples a bindless heap texture with the material's shared sampler.
vec4 tex(uint index, uint samp) {
    return texture(sampler2D(gTextures[nonuniformEXT(index)], gSamplers[nonuniformEXT(samp)]), vUV);
}

// perturbNormal applies a tangent-space normal (mapN in [-1,1]) to the geometric
// normal N using a cotangent frame derived from screen-space derivatives — no vertex
// TANGENT attribute required (Christian Schüler's derivative maps).
vec3 perturbNormal(vec3 N, vec3 mapN) {
    vec3 dp1 = dFdx(vWorldPos);
    vec3 dp2 = dFdy(vWorldPos);
    vec2 duv1 = dFdx(vUV);
    vec2 duv2 = dFdy(vUV);
    vec3 dp2perp = cross(dp2, N);
    vec3 dp1perp = cross(N, dp1);
    vec3 T = dp2perp * duv1.x + dp1perp * duv2.x;
    vec3 B = dp2perp * duv1.y + dp1perp * duv2.y;
    float invmax = inversesqrt(max(dot(T, T), dot(B, B)));
    mat3 TBN = mat3(T * invmax, B * invmax, N);
    return normalize(TBN * mapN);
}

float distributionGGX(vec3 N, vec3 H, float rough) {
    float a = rough * rough;
    float a2 = a * a;
    float ndh = max(dot(N, H), 0.0);
    float d = ndh * ndh * (a2 - 1.0) + 1.0;
    return a2 / max(PI * d * d, 1e-5);
}

float geometrySchlickGGX(float ndv, float rough) {
    float r = rough + 1.0;
    float k = (r * r) / 8.0;
    return ndv / (ndv * (1.0 - k) + k);
}

float geometrySmith(vec3 N, vec3 V, vec3 L, float rough) {
    return geometrySchlickGGX(max(dot(N, V), 0.0), rough) * geometrySchlickGGX(max(dot(N, L), 0.0), rough);
}

vec3 fresnelSchlick(float cosT, vec3 f0) {
    return f0 + (1.0 - f0) * pow(clamp(1.0 - cosT, 0.0, 1.0), 5.0);
}

// cookTorrance returns one light's outgoing radiance. diffuseScale attenuates the
// diffuse (transmitted) term for glass while keeping the specular reflection.
vec3 cookTorrance(vec3 N, vec3 V, vec3 L, vec3 radiance, vec3 albedo, float metallic, float rough, float diffuseScale) {
    vec3 H = normalize(V + L);
    vec3 f0 = mix(vec3(0.04), albedo, metallic);
    float ndf = distributionGGX(N, H, rough);
    float g = geometrySmith(N, V, L, rough);
    vec3 f = fresnelSchlick(max(dot(H, V), 0.0), f0);
    vec3 spec = (ndf * g * f) / max(4.0 * max(dot(N, V), 0.0) * max(dot(N, L), 0.0), 1e-4);
    vec3 kd = (vec3(1.0) - f) * (1.0 - metallic);
    float ndl = max(dot(N, L), 0.0);
    return (kd * albedo / PI * diffuseScale + spec) * radiance * ndl;
}

void main() {
    Material m = MatBuf(pc.root.materials).v[vMat];
    vec4 base = sampleBase(m.color, m.flags, m.colorMap, m.colorSampler);
    vec3 albedo = base.rgb;

    // Metallic/roughness maps modulate the factors (glTF: metallic in .b, roughness
    // in .g; grayscale maps work with any channel).
    float metallic = m.metallic;
    if ((m.flags & MAT_METAL_MAP) != 0u) metallic *= tex(m.metalMap, m.metalSampler).b;
    float roughness = m.roughness;
    if ((m.flags & MAT_ROUGH_MAP) != 0u) roughness *= tex(m.roughMap, m.roughSampler).g;

    LightBuf L = pc.root.lights;
    vec3 N = normalize(vNormal);
    if ((m.flags & MAT_NORMAL_MAP) != 0u) {
        N = perturbNormal(N, tex(m.normalMap, m.normalSampler).xyz * 2.0 - 1.0);
    }
    vec3 V = normalize(pc.root.eye.xyz - vWorldPos);

    // Transmission (glass): suppress the diffuse (transmitted) term, keep specular,
    // and make the surface see-through via alpha. Not a true refraction — a cheap
    // approximation of KHR_materials_transmission.
    float diffuseScale = 1.0 - m.transmission;

    uint shadowSamp = pc.root.shadowSampler;
    bool receives = (vFlags & FLAG_RECEIVES_SHADOW) != 0u;

    vec3 lo = vec3(0.0);
    for (uint i = 0u; i < L.numDir; i++) {
        DirLight dl = L.dirs[i];
        float sh = receives ? shadowFactor(dl.shadowVP, dl.shadowMap, vWorldPos, shadowSamp) : 1.0;
        lo += sh * cookTorrance(N, V, normalize(-dl.dir.xyz), dl.color.rgb * dl.color.w, albedo, metallic, roughness, diffuseScale);
    }
    for (uint i = 0u; i < L.numPoint; i++) {
        PointLight pl = L.points[i];
        vec3 d = pl.pos.xyz - vWorldPos;
        float dist = length(d);
        float range = max(pl.pos.w, 0.0001);
        float atten = clamp(1.0 - dist / range, 0.0, 1.0);
        atten *= atten;
        float sh = receives ? pointShadowFactor(pl, vWorldPos, shadowSamp) : 1.0;
        lo += sh * cookTorrance(N, V, d / max(dist, 0.0001), pl.color.rgb * pl.color.w * atten, albedo, metallic, roughness, diffuseScale);
    }
    for (uint i = 0u; i < L.numSpot; i++) {
        SpotLight sl = L.spots[i];
        vec3 d = sl.pos.xyz - vWorldPos;
        float dist = length(d);
        vec3 Ldir = d / max(dist, 1e-4);
        float atten = spotAttenuation(sl, vWorldPos, Ldir, dist);
        if (atten <= 0.0) continue;
        float sh = receives ? shadowFactor(sl.shadowVP, sl.shadowMap, vWorldPos, shadowSamp) : 1.0;
        lo += sh * cookTorrance(N, V, Ldir, sl.color.rgb * sl.color.w * atten, albedo, metallic, roughness, diffuseScale);
    }

    vec3 ambient = L.ambient.rgb * albedo * diffuseScale;
    float alpha = base.a;
    if (m.transmission > 0.0) {
        alpha = max(1.0 - m.transmission, 0.08); // glass never fully invisible
    }
    outColor = vec4(linearToSrgb(ambient + lo + m.emissive.rgb), alpha);
}
