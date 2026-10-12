#version 460
// particle_shift.comp.glsl — a custom particle update, written the way an application
// writes one: it moves every living particle by the shift its Params hold, once a step.
#define PARTICLE_PARAMS vec3 shift;
#include <pix/particles/simulation.glsl>

void update(inout Particle particle, float dt) {
    particle.position += pc.shift;
}
