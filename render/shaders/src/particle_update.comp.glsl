#version 460
// particle_update.comp.glsl — pix's default particle update, for a ParticleUpdate with no
// Shader: gravity, drag, and size and opacity over life, configured by its fields.
//
// Linear curves are evaluated at the particle's clamped age / lifetime and multiply its
// birth value.
#define PARTICLE_PARAMS     \
    vec3 gravity;           \
    float drag;             \
    uint sizeEnabled;       \
    float sizeStart;        \
    float sizeEnd;          \
    uint opacityEnabled;    \
    float opacityStart;     \
    float opacityEnd;
#include <pix/particles/simulation.glsl>

void update(inout Particle particle, float dt) {
    particle.velocity += pc.gravity * dt;
    particle.velocity *= exp(-pc.drag * dt);
    particle.position += particle.velocity * dt;

    float t = clamp(particle.age / particle.lifetime, 0.0, 1.0);
    if (pc.sizeEnabled != 0u) {
        particle.scale = particle.initialScale * mix(pc.sizeStart, pc.sizeEnd, t);
    }
    if (pc.opacityEnabled != 0u) {
        particle.color.a = particle.initialColor.a * mix(pc.opacityStart, pc.opacityEnd, t);
    }
}
