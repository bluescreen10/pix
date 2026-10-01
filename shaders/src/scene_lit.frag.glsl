#version 460
#extension GL_EXT_buffer_reference : require
#extension GL_EXT_buffer_reference2 : require
#extension GL_EXT_scalar_block_layout : require
#extension GL_EXT_nonuniform_qualifier : require
#extension GL_EXT_shader_explicit_arithmetic_types_int64 : require
#extension GL_GOOGLE_include_directive : require

// BlinnPhongMaterial: ambient + per-light diffuse (N·L) + Blinn-Phong specular.
#include "material_common.glsl"

layout(location = 0) out vec4 outColor;

// Material mirrors pix.blinnPhongRecord (64 bytes).
struct Material {
    vec4 color;
    vec4 emissive;
    float specular;
    float shininess;
    uint colorMap;
    uint samp;
    uint flags;
    uint pad0;
    uint pad1;
    uint pad2;
};
layout(buffer_reference, scalar) readonly buffer MatBuf { Material v[]; };

vec3 blinnPhong(vec3 N, vec3 V, vec3 L, vec3 radiance, vec3 albedo, float specStrength, float shininess) {
    float diff = max(dot(N, L), 0.0);
    vec3 H = normalize(L + V);
    float spec = pow(max(dot(N, H), 0.0), shininess) * specStrength;
    // Gate the highlight on the light actually reaching the surface. Without this a
    // light BEHIND a surface still puts a specular glint on it whenever the half vector
    // happens to line up, which is wrong on its own account and also means the whole
    // term cannot be skipped when the light is behind — and skipping it is what lets the
    // shadow lookup be skipped with it.
    return radiance * (albedo * diff + vec3(spec) * step(0.0, dot(N, L)));
}

void main() {
    Material m = MatBuf(pc.materials).v[vMat];
    vec4 base = sampleBase(m.color, m.flags, m.colorMap, m.samp);
    vec3 albedo = base.rgb;

    LightBuf L = pc.lights;
    vec3 N = normalize(vNormal);
    vec3 V = normalize(pc.eye.xyz - vWorldPos);

    uint shadowSamp = pc.shadowSampler;
    bool receives = (vFlags & FLAG_RECEIVES_SHADOW) != 0u;

    vec3 lit = L.ambient.rgb * albedo;
    float viewDist = length(pc.eye.xyz - vWorldPos);
    for (uint i = 0u; i < L.numDir; i++) {
        vec3 Ldir = normalize(-L.dirs[i].dir.xyz);
        // Nothing reaches a surface turned away from the light, so neither the shading
        // nor the shadow lookup that would scale it is worth paying for.
        if (dot(N, Ldir) <= 0.0) continue;
        float sh = receives ? dirShadowFactor(L, i, vWorldPos, N, viewDist, shadowSamp) : 1.0;
        vec3 radiance = L.dirs[i].color.rgb * L.dirs[i].color.w * dirMask(L, i, vWorldPos);
        lit += sh * blinnPhong(N, V, Ldir, radiance, albedo, m.specular, m.shininess);
    }
    for (uint i = 0u; i < L.numPoint; i++) {
        PointLight pl = L.points[i];
        vec3 d = pl.pos.xyz - vWorldPos;
        float dist = length(d);
        float range = max(pl.pos.w, 0.0001);
        float atten = clamp(1.0 - dist / range, 0.0, 1.0);
        atten *= atten;
        float sh = receives ? pointShadowFactor(pl, vWorldPos, shadowSamp) : 1.0;
        lit += sh * blinnPhong(N, V, d / max(dist, 0.0001), pl.color.rgb * pl.color.w * atten, albedo, m.specular, m.shininess);
    }
    for (uint i = 0u; i < L.numSpot; i++) {
        SpotLight sl = L.spots[i];
        vec3 d = sl.pos.xyz - vWorldPos;
        float dist = length(d);
        vec3 Ldir = d / max(dist, 1e-4);
        float atten = spotAttenuation(sl, vWorldPos, Ldir, dist);
        if (atten <= 0.0) continue;
        float sh = receives ? shadowFactor(sl.shadowVP, sl.shadowMap, vWorldPos, shadowSamp, sl.shadowBias, 0u, 1u, 1u, SHADOW_FILTER_HARD) : 1.0;
        lit += sh * blinnPhong(N, V, Ldir, sl.color.rgb * sl.color.w * atten, albedo, m.specular, m.shininess);
    }

    lit = applyFog(lit + m.emissive.rgb, vWorldPos, pc.eye.xyz, L.fogColor, L.fogParams);
    outColor = vec4(lit, base.a); // linear: the target encodes it for display
}
