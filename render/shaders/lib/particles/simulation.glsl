// particles/simulation.glsl — a particle system's update kernel. It owns main(), the
// bindings and the bookkeeping: it ages every living particle, drops the ones whose
// time is up, keeps the survivors and this step's newborns, and counts them for the
// draw. The shader including it supplies only what a living particle does each step:
//
//     #version 460
//     #define PARTICLE_PARAMS \
//         vec3 wind;          \
//         float turbulence;
//     #include <pix/particles/simulation.glsl>
//
//     void update(inout Particle particle, float dt) {
//         particle.velocity += pc.wind * dt;
//         particle.position += particle.velocity * dt;
//     }
//
// update runs once a step for each particle still alive after ageing, with age already
// advanced by dt. Setting age to lifetime kills the particle. Newborns are not updated
// in the step they are born: they render first as Spawn left them.
//
// PARTICLE_PARAMS, optional, declares the fields ParticleUpdate.Params holds, read as
// pc.<field>. They follow a 48-byte root in push constants, scalar layout, so a field is
// read at its offset from the start of Params; 128 bytes is the least push-constant
// space any device offers, which leaves 80 for Params.
#ifndef PIX_PARTICLES_SIMULATION_GLSL
#define PIX_PARTICLES_SIMULATION_GLSL

#extension GL_EXT_buffer_reference : require
#extension GL_EXT_buffer_reference2 : require
#extension GL_EXT_scalar_block_layout : require
#extension GL_EXT_shader_explicit_arithmetic_types_int64 : require

#ifndef PARTICLE_PARAMS
#define PARTICLE_PARAMS
#endif

layout(local_size_x = 64) in;

// Particle is scenes.ParticleRecord, field for field.
struct Particle {
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

struct ParticleDrawCommand {
    uint indexCount;
    uint instanceCount;
    uint firstIndex;
    int  vertexOffset;
    uint firstInstance;
};

layout(buffer_reference, scalar) readonly buffer ParticlesToRead { Particle particles[]; };
layout(buffer_reference, scalar) buffer ParticlesToWrite { Particle particles[]; };
layout(buffer_reference, scalar) buffer ParticleDrawCommands { ParticleDrawCommand commands[]; };

// source holds last step's particles and destination receives this step's; newborns
// holds the births staged since. draw counts what destination holds.
layout(push_constant, scalar) uniform PC {
    ParticlesToRead source;
    ParticlesToWrite destination;
    ParticlesToRead newborns;
    ParticleDrawCommands draw;
    uint capacity;
    uint newbornCount;
    float dt;
    uint rootPadding;
    PARTICLE_PARAMS
} pc;

void update(inout Particle particle, float dt);

void keepParticle(Particle particle) {
    uint slot = atomicAdd(pc.draw.commands[0].instanceCount, 1u);
    pc.destination.particles[slot] = particle;
}

// One thread per slot of source, then one per newborn. A dispatch sized to the living
// count alone would need that count back from the GPU first.
void main() {
    uint thread = gl_GlobalInvocationID.x;
    if (thread >= pc.capacity) {
        uint newborn = thread - pc.capacity;
        if (newborn < pc.newbornCount) {
            keepParticle(pc.newborns.particles[newborn]);
        }
        return;
    }

    // A slot never written holds zeros, lifetime 0, so it is dead like an expired one.
    Particle particle = pc.source.particles[thread];
    if (particle.age >= particle.lifetime) {
        return;
    }
    particle.age += pc.dt;
    if (particle.age >= particle.lifetime) {
        return;
    }
    update(particle, pc.dt);
    if (particle.age >= particle.lifetime) {
        return;
    }
    keepParticle(particle);
}

#endif // PIX_PARTICLES_SIMULATION_GLSL
