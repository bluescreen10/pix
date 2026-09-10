#version 460
#extension GL_EXT_buffer_reference : require
#extension GL_EXT_buffer_reference2 : require
#extension GL_EXT_scalar_block_layout : require
#extension GL_EXT_shader_explicit_arithmetic_types_int64 : require

// Vertex-pulling draw for a particle container (ParticleFaceParticle only): each
// instance is one compacted particle record, oriented by its own rotation/scale and
// then the container's single world transform — no per-drawable indirection, since a
// container has exactly one geometry and one material. Pairs only with
// particle_basic.frag.glsl (see particle_common.glsl): particles have their own
// push-constant/varying contract now, not the mesh path's DrawRoot repurposed via
// spare padding — vColor here is the particle's actual per-instance color+alpha
// (ParticleRecord.color), which the shared mesh contract had no room to carry.
struct GeoDesc {
    uint positionBase;
    uint attributeBase;
    uint indexBase;
    uint indexCount;
    uint flags;
    uint pad;
};
struct ParticleRecord {
    vec3  position;
    vec3  velocity;
    vec4  rotation;
    vec3  scale;
    vec4  color;
    vec4  data;
    float age;
    float lifetime;
    vec3  initialScale;
    vec4  initialColor;
};

layout(buffer_reference, scalar) readonly buffer PosBuf { float v[]; };
layout(buffer_reference, scalar) readonly buffer AttrBuf { uint v[]; };
layout(buffer_reference, scalar) readonly buffer DescBuf { GeoDesc v[]; };
layout(buffer_reference, scalar) readonly buffer ModelBuf { mat4 v[]; };
layout(buffer_reference, scalar) readonly buffer ParticleBuf { ParticleRecord v[]; };

// Matches particle_common.glsl's PC exactly (see that file's comment on why this is
// declared separately rather than shared via #include) and particleDrawRoot in
// particle.go.
layout(push_constant, scalar) uniform PC {
    mat4 viewProj;
    PosBuf pos;
    AttrBuf attr;
    DescBuf descs;
    ModelBuf models;
    ParticleBuf particles;
    uint64_t materials;
    uint64_t lights;
    vec4 eye;
    uint geometryID;
    uint materialID;
    uint transformID;
    uint pad0;
} pc;

layout(location = 0) out vec4 vColor;
layout(location = 1) out vec2 vUV;
layout(location = 2) out vec3 vWorldPos;

const uint FLAG_UV = 2u;

// Standard quaternion-vector rotation (q * v * conj(q)), matching glm.Quat's [x,y,z,w].
vec3 rotate(vec4 q, vec3 v) {
    vec3 u = q.xyz;
    float s = q.w;
    return 2.0 * dot(u, v) * u + (s * s - dot(u, u)) * v + 2.0 * s * cross(u, v);
}

void main() {
    ParticleRecord p = pc.particles.v[uint(gl_InstanceIndex)];
    GeoDesc g = pc.descs.v[pc.geometryID];
    mat4 m = pc.models.v[pc.transformID];
    vColor = p.color;

    uint vi = uint(gl_VertexIndex);
    uint pb = g.positionBase + vi * 3u;
    vec3 lp = vec3(pc.pos.v[pb], pc.pos.v[pb + 1u], pc.pos.v[pb + 2u]);

    uint ab = g.attributeBase * 4u + vi * 4u;
    vUV = vec2(0.0);
    if ((g.flags & FLAG_UV) != 0u) {
        vUV = vec2(uintBitsToFloat(pc.attr.v[ab + 2u]), uintBitsToFloat(pc.attr.v[ab + 3u]));
    }

    vec3 particleSpace = rotate(p.rotation, lp * p.scale);
    vec4 wp = m * vec4(particleSpace + p.position, 1.0);
    vWorldPos = wp.xyz;
    gl_Position = pc.viewProj * wp;
}
