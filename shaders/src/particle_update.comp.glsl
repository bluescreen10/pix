#version 460
#extension GL_EXT_buffer_reference : require
#extension GL_EXT_buffer_reference2 : require
#extension GL_EXT_scalar_block_layout : require
#extension GL_EXT_shader_explicit_arithmetic_types_int64 : require

// GPU-resident particle simulation: ages, integrates, and evaluates Pix's default
// behaviors (gravity, drag, size/opacity over life) for every living particle, then
// stream-compacts survivors — plus this step's newborns — into the other half of a
// ping-pong pair. One thread per capacity slot handles survivors; the remaining
// threads (indices >= capacity) each consume one staged newborn. Unlike
// scene_cull.comp's per-frame stateless visibility compaction, particle state must
// persist frame to frame, which is why this needs two buffers rather than one.
//
// Dispatched at a fixed capacity + pendingCount threads (both CPU-known), not the
// true alive count: an indirect-dispatch sized to the GPU's own compacted count
// would avoid processing dead slots, but would need either an indirect-dispatch
// chain or a CPU readback of last frame's count. Capacity-sized dispatch avoids
// both at the cost of visiting up to `capacity` threads even when few particles are
// alive — a deliberate first-implementation tradeoff (see docs/particle-system.md).
layout(local_size_x = 64) in;

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

layout(buffer_reference, scalar) readonly buffer SrcBuf { ParticleRecord v[]; };
layout(buffer_reference, scalar) buffer DstBuf { ParticleRecord v[]; };
layout(buffer_reference, scalar) readonly buffer PendingBuf { ParticleRecord v[]; };
layout(buffer_reference, scalar) buffer IndirectBuf { IndirectCmd v[]; };

// Pushed inline rather than behind a device address: it fits in push constants on
// every backend, so the shader reads its parameters directly instead of chasing a
// pointer to reach them. Fields that are themselves addresses stay addresses — those
// point at unbounded arrays, so that indirection is inherent.
layout(push_constant, scalar) uniform PC {
    SrcBuf src;
    DstBuf dst;
    PendingBuf pending;
    IndirectBuf indirect;
    uint capacity;
    uint pendingCount;
    float dt;
    vec3 gravity;
    float drag;
    uint sizeEnabled;
    float sizeStart;
    float sizeEnd;
    uint opacityEnabled;
    float opacityStart;
    float opacityEnd;
} pc;

void appendSurvivor(ParticleRecord p) {
    uint slot = atomicAdd(pc.indirect.v[0].instanceCount, 1u);
    pc.dst.v[slot] = p;
}

void main() {
    uint tid = gl_GlobalInvocationID.x;

    if (tid < pc.capacity) {
        ParticleRecord p = pc.src.v[tid];
        // A never-born or already-dead slot: buffers are zero-initialized at
        // allocation (lifetime 0), so an untouched slot is dead by construction,
        // not by an uninitialized read.
        if (p.age >= p.lifetime) return;

        p.age += pc.dt;
        p.velocity += pc.gravity * pc.dt;
        p.velocity *= exp(-pc.drag * pc.dt);
        p.position += p.velocity * pc.dt;
        if (p.age >= p.lifetime) return; // died this step: no partial final update

        float t = clamp(p.age / p.lifetime, 0.0, 1.0);
        if (pc.sizeEnabled != 0u) {
            p.scale = p.initialScale * mix(pc.sizeStart, pc.sizeEnd, t);
        }
        if (pc.opacityEnabled != 0u) {
            p.color.a = p.initialColor.a * mix(pc.opacityStart, pc.opacityEnd, t);
        }
        appendSurvivor(p);
    } else {
        uint ni = tid - pc.capacity;
        if (ni >= pc.pendingCount) return;
        // Newborns are appended without ageing or integrating them this step —
        // they were already placed at age 0 with their spawn-time appearance.
        appendSurvivor(pc.pending.v[ni]);
    }
}
