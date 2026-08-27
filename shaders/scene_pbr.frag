#version 460
#extension GL_EXT_buffer_reference : require
#extension GL_EXT_buffer_reference2 : require
#extension GL_EXT_scalar_block_layout : require
#extension GL_EXT_nonuniform_qualifier : require
#extension GL_EXT_shader_explicit_arithmetic_types_int64 : require
#extension GL_GOOGLE_include_directive : require

// PBRMaterial: metallic-roughness Cook-Torrance (GGX, Smith, Schlick), flat ambient.
#include "material_common.glsl"

// Material mirrors pix.pbrRecord (64 bytes).
struct Material {
    vec4 baseColor;
    vec4 emissive;
    float metallic;
    float roughness;
    uint colorMap;
    uint samp;
    uint flags;
    uint pad0;
    uint pad1;
    uint pad2;
};
layout(buffer_reference, scalar) readonly buffer MatBuf { Material v[]; };

const float PI = 3.14159265359;

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

vec3 cookTorrance(vec3 N, vec3 V, vec3 L, vec3 radiance, vec3 albedo, float metallic, float rough) {
    vec3 H = normalize(V + L);
    vec3 f0 = mix(vec3(0.04), albedo, metallic);
    float ndf = distributionGGX(N, H, rough);
    float g = geometrySmith(N, V, L, rough);
    vec3 f = fresnelSchlick(max(dot(H, V), 0.0), f0);
    vec3 spec = (ndf * g * f) / max(4.0 * max(dot(N, V), 0.0) * max(dot(N, L), 0.0), 1e-4);
    vec3 kd = (vec3(1.0) - f) * (1.0 - metallic);
    float ndl = max(dot(N, L), 0.0);
    return (kd * albedo / PI + spec) * radiance * ndl;
}

void main() {
    Material m = MatBuf(pc.root.materials).v[vMat];
    vec4 base = sampleBase(m.baseColor, m.flags, m.colorMap, m.samp);
    vec3 albedo = base.rgb;

    LightBuf L = pc.root.lights;
    vec3 N = normalize(vNormal);
    vec3 V = normalize(pc.root.eye.xyz - vWorldPos);

    vec3 lo = vec3(0.0);
    for (uint i = 0u; i < L.numDir; i++) {
        DirLight dl = L.dirs[i];
        lo += cookTorrance(N, V, normalize(-dl.dir.xyz), dl.color.rgb * dl.color.w, albedo, m.metallic, m.roughness);
    }
    for (uint i = 0u; i < L.numPoint; i++) {
        PointLight pl = L.points[i];
        vec3 d = pl.pos.xyz - vWorldPos;
        float dist = length(d);
        float range = max(pl.pos.w, 0.0001);
        float atten = clamp(1.0 - dist / range, 0.0, 1.0);
        atten *= atten;
        lo += cookTorrance(N, V, d / max(dist, 0.0001), pl.color.rgb * pl.color.w * atten, albedo, m.metallic, m.roughness);
    }

    vec3 ambient = L.ambient.rgb * albedo;
    outColor = vec4(linearToSrgb(ambient + lo + m.emissive.rgb), base.a);
}
