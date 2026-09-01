#version 460
#extension GL_EXT_buffer_reference : require
#extension GL_EXT_buffer_reference2 : require
#extension GL_EXT_scalar_block_layout : require
#extension GL_EXT_nonuniform_qualifier : require
#extension GL_EXT_shader_explicit_arithmetic_types_int64 : require
#extension GL_GOOGLE_include_directive : require

// BlinnPhongMaterial: ambient + per-light diffuse (N·L) + Blinn-Phong specular.
#include "material_common.glsl"

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
    return radiance * (albedo * diff + vec3(spec));
}

void main() {
    Material m = MatBuf(pc.root.materials).v[vMat];
    vec4 base = sampleBase(m.color, m.flags, m.colorMap, m.samp);
    vec3 albedo = base.rgb;

    LightBuf L = pc.root.lights;
    vec3 N = normalize(vNormal);
    vec3 V = normalize(pc.root.eye.xyz - vWorldPos);

    uint shadowSamp = pc.root.shadowSampler;
    bool receives = (vFlags & FLAG_RECEIVES_SHADOW) != 0u;

    vec3 lit = L.ambient.rgb * albedo;
    for (uint i = 0u; i < L.numDir; i++) {
        DirLight dl = L.dirs[i];
        float sh = receives ? shadowFactor(dl.shadowVP, dl.shadowMap, vWorldPos, shadowSamp) : 1.0;
        lit += sh * blinnPhong(N, V, normalize(-dl.dir.xyz), dl.color.rgb * dl.color.w, albedo, m.specular, m.shininess);
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
        float sh = receives ? shadowFactor(sl.shadowVP, sl.shadowMap, vWorldPos, shadowSamp) : 1.0;
        lit += sh * blinnPhong(N, V, Ldir, sl.color.rgb * sl.color.w * atten, albedo, m.specular, m.shininess);
    }

    outColor = vec4(linearToSrgb(lit + m.emissive.rgb), base.a);
}
