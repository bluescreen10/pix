// particle_common.glsl — shared by particle fragment shaders, mirroring
// material_common.glsl's shape (bindless heap + light table via lighting.glsl, the
// push-constant root, the vertex→fragment varyings, a sampleBase-style helper) but
// with its own contract: a particle container has exactly one geometry/material for
// the whole draw (no per-instance Drawable table, so geometryID/materialID/
// transformID are plain scalars, not indices into anything), and vColor is vec4 —
// the particle's own per-instance color+alpha (see particleRecord in particle.go),
// not a baked-per-vertex color forced to opaque. This is what material_common.glsl's
// shared mesh contract structurally cannot carry (see docs/particle-system.md).
#ifndef PARTICLE_COMMON_GLSL
#define PARTICLE_COMMON_GLSL

#include "lighting.glsl"

// Matches ParticleDrawRoot in particle_draw.vert.glsl and particleDrawRoot in
// particle.go exactly — one shared push-constant block for both stages of a
// particle draw call, same discipline scene_draw.vert.glsl/material_common.glsl
// already use (each declares its own copy rather than sharing one header across
// stages, so the vertex file stays self-contained without pulling in fragment-only
// declarations like sampleBase).
layout(push_constant, scalar) uniform PC {
    mat4 viewProj;
    uint64_t pos;
    uint64_t attr;
    uint64_t descs;
    uint64_t models;
    uint64_t particles;
    uint64_t materials;
    LightBuf lights;
    vec4 eye;
    uint geometryID;
    uint materialID;
    uint transformID;
    // time is elapsed seconds since the scene's clock started (Scene.clockStart),
    // passed to every vertex/fragment shader pair unconditionally — read it or not.
    float time;
    // order is read by the vertex stage only (see particle_draw.vert.glsl).
    uint64_t order;
} pc;

layout(location = 0) in vec4 vColor;
layout(location = 1) in vec2 vUV;
layout(location = 2) in vec3 vWorldPos;

const uint MAT_COLOR_MAP = 1u;

// sampleParticleBase returns base color × the particle's own color+alpha, modulated
// by the color map when the MAT_COLOR_MAP flag is set — the particle-path analogue
// of material_common.glsl's sampleBase, but multiplying all four channels (base *
// vColor) instead of forcing alpha to 1.
vec4 sampleParticleBase(vec4 base, uint flags, uint colorMap, uint samp) {
    vec4 c = base * vColor;
    if ((flags & MAT_COLOR_MAP) != 0u) {
        c *= texture(sampler2D(gTextures[nonuniformEXT(colorMap)], gSamplers[nonuniformEXT(samp)]), vUV);
    }
    return c;
}

#endif // PARTICLE_COMMON_GLSL
