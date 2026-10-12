#version 460
#extension GL_EXT_buffer_reference : require
#extension GL_EXT_buffer_reference2 : require
#extension GL_EXT_scalar_block_layout : require
#extension GL_EXT_shader_explicit_arithmetic_types_int64 : require

// particle_sort_keys — the first pass of sorting a particle system back to front. It
// writes one entry per slot of the system's order buffer: for a living particle, its
// center's depth along the camera's view direction and its index; for every slot past
// the living ones, the lowest key there is, which the descending sort
// (particle_sort_step) moves past every living particle — including one behind the
// camera, whose depth is negative — so the draw's first instanceCount entries are the
// living particles, farthest first.
//
// Depth, not distance: of two particles side by side, the one farther off to the side
// is farther from the camera without being behind the other.
layout(local_size_x = 64) in;

// Matches ParticleRecord in particle_update.comp.glsl and scenes.ParticleRecord.
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
struct IndirectCmd { uint indexCount; uint instanceCount; uint firstIndex; int vertexOffset; uint firstInstance; };
struct SortEntry { float key; uint particle; };

layout(buffer_reference, scalar) readonly buffer ParticleBuf { ParticleRecord v[]; };
layout(buffer_reference, scalar) readonly buffer IndirectBuf { IndirectCmd v[]; };
layout(buffer_reference, scalar) readonly buffer ModelBuf { mat4 v[]; };
layout(buffer_reference, scalar) writeonly buffer OrderBuf { SortEntry v[]; };

// Matches particleSortKeysRoot in particle_gpu.go.
layout(push_constant, scalar) uniform PC {
    ParticleBuf particles;
    IndirectBuf indirect;
    ModelBuf models;
    OrderBuf order;
    vec4 depthPlane; // dot(depthPlane.xyz, p) + depthPlane.w is p's view depth
    uint transformID;
    uint count; // entries in order: the capacity rounded up to a power of two
    uint pad0;
    uint pad1;
} pc;

// emptySlot is the key of a slot past the living particles: the lowest finite float.
const float emptySlot = -3.402823466e38;

void main() {
    uint i = gl_GlobalInvocationID.x;
    if (i >= pc.count) {
        return;
    }
    float key = emptySlot;
    if (i < pc.indirect.v[0].instanceCount) {
        vec3 center = (pc.models.v[pc.transformID] * vec4(pc.particles.v[i].position, 1.0)).xyz;
        key = dot(pc.depthPlane.xyz, center) + pc.depthPlane.w;
    }
    pc.order.v[i] = SortEntry(key, i);
}
