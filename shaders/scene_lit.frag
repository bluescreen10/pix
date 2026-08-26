#version 460
#extension GL_EXT_buffer_reference : require
#extension GL_EXT_buffer_reference2 : require
#extension GL_EXT_scalar_block_layout : require
#extension GL_EXT_nonuniform_qualifier : require
#extension GL_EXT_shader_explicit_arithmetic_types_int64 : require

// Bindless heap (set 0): sampled images at binding 0, samplers at binding 2.
layout(set = 0, binding = 0) uniform texture2D gTextures[];
layout(set = 0, binding = 2) uniform sampler gSamplers[];

const uint MAX_DIR = 4u;
const uint MAX_POINT = 16u;

struct Material {
    vec4 baseColor;
    uint colorMap;
    uint samp;
    uint flags;
    uint pad;
};
layout(buffer_reference, scalar) readonly buffer MatBuf { Material v[]; };

struct DirLight { vec4 dir; vec4 color; };     // dir.xyz = direction the light travels; color.w = intensity
struct PointLight { vec4 pos; vec4 color; };   // pos.xyz world, pos.w = range; color.w = intensity
layout(buffer_reference, scalar) readonly buffer LightBuf {
    vec4 ambient;
    uint numDir;
    uint numPoint;
    uint pad0;
    uint pad1;
    DirLight dirs[MAX_DIR];
    PointLight points[MAX_POINT];
};

// Same layout as DrawRoot in scene_draw.vert; vertex-stage pointers are opaque.
layout(buffer_reference, scalar) readonly buffer DrawRoot {
    mat4 viewProj;
    uint64_t pos;
    uint64_t attr;
    uint64_t descs;
    uint64_t models;
    uint64_t drawables;
    uint64_t visible;
    MatBuf materials;
    LightBuf lights;
    uint regionBase;
    vec4 eye;
};
layout(push_constant) uniform PC { DrawRoot root; } pc;

layout(location = 0) in vec3 vColor;
layout(location = 1) in vec2 vUV;
layout(location = 2) flat in uint vMat;
layout(location = 3) in vec3 vWorldPos;
layout(location = 4) in vec3 vNormal;
layout(location = 0) out vec4 outColor;

const uint MAT_COLOR_MAP = 1u;

// blinnPhong returns the diffuse+specular contribution of one light.
vec3 blinnPhong(vec3 N, vec3 V, vec3 L, vec3 radiance, vec3 albedo) {
    float diff = max(dot(N, L), 0.0);
    vec3 H = normalize(L + V);
    float spec = pow(max(dot(N, H), 0.0), 32.0) * 0.3;
    return radiance * (albedo * diff + vec3(spec));
}

void main() {
    Material mat = pc.root.materials.v[vMat];
    vec4 base = mat.baseColor * vec4(vColor, 1.0);
    if ((mat.flags & MAT_COLOR_MAP) != 0u) {
        base *= texture(sampler2D(gTextures[nonuniformEXT(mat.colorMap)], gSamplers[nonuniformEXT(mat.samp)]), vUV);
    }
    vec3 albedo = base.rgb;

    LightBuf L = pc.root.lights;
    vec3 N = normalize(vNormal);
    vec3 V = normalize(pc.root.eye.xyz - vWorldPos);

    vec3 lit = L.ambient.rgb * albedo;
    for (uint i = 0u; i < L.numDir; i++) {
        DirLight dl = L.dirs[i];
        lit += blinnPhong(N, V, normalize(-dl.dir.xyz), dl.color.rgb * dl.color.w, albedo);
    }
    for (uint i = 0u; i < L.numPoint; i++) {
        PointLight pl = L.points[i];
        vec3 d = pl.pos.xyz - vWorldPos;
        float dist = length(d);
        float range = max(pl.pos.w, 0.0001);
        float atten = clamp(1.0 - dist / range, 0.0, 1.0);
        atten *= atten;
        lit += blinnPhong(N, V, d / max(dist, 0.0001), pl.color.rgb * pl.color.w * atten, albedo);
    }

    outColor = vec4(lit, base.a);
}
