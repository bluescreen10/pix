#version 460
#extension GL_EXT_scalar_block_layout : require
#extension GL_EXT_shader_explicit_arithmetic_types_int64 : require

// Colors an object or a triangle by index, from a small fixed palette rather than a
// raw hash-to-RGB (which tends to look muddy/oversaturated) — see recordDebugIDView.
// mode 0: vObjectID (the drawable index, forwarded flat from the vertex stage — see
// scene_debug_id.vert.glsl) — every triangle of one drawable gets the same color.
// mode 1: gl_PrimitiveID, a free fragment-stage builtin (no vertex forwarding needed)
// — every triangle within a drawable gets its own color, wrapping through the palette.

layout(push_constant, scalar) uniform PC {
    mat4 viewProj;
    uint64_t pos, descs, models, drawables, visible;
    uint mode;
    uint pad0;
} pc;

layout(location = 0) flat in uint vObjectID;
layout(location = 0) out vec4 outColor;

const vec3 palette[24] = vec3[](
    vec3(0.90, 0.30, 0.30), vec3(0.30, 0.60, 0.90), vec3(0.40, 0.80, 0.40),
    vec3(0.95, 0.70, 0.20), vec3(0.65, 0.40, 0.85), vec3(0.30, 0.85, 0.85),
    vec3(0.90, 0.50, 0.70), vec3(0.55, 0.55, 0.25), vec3(0.20, 0.45, 0.30),
    vec3(0.85, 0.85, 0.35), vec3(0.55, 0.40, 0.30), vec3(0.75, 0.75, 0.75),
    vec3(0.95, 0.40, 0.15), vec3(0.30, 0.40, 0.70), vec3(0.60, 0.85, 0.60),
    vec3(0.80, 0.20, 0.55), vec3(0.45, 0.65, 0.85), vec3(0.70, 0.55, 0.35),
    vec3(0.35, 0.75, 0.55), vec3(0.85, 0.60, 0.85), vec3(0.60, 0.30, 0.30),
    vec3(0.25, 0.55, 0.55), vec3(0.90, 0.80, 0.50), vec3(0.55, 0.55, 0.85)
);

// A cheap integer hash (murmur-style avalanche) before the modulo — without it,
// gl_PrimitiveID's sequential values would cycle through the palette in visible
// bands (every 24 triangles repeating the same run of colors) instead of looking
// evenly shuffled. Object ids don't strictly need this (they aren't usually spatially
// sequential), but applying it uniformly means one lookup function for both modes.
uint hash(uint x) {
    x ^= x >> 16;
    x *= 0x7feb352du;
    x ^= x >> 15;
    x *= 0x846ca68bu;
    x ^= x >> 16;
    return x;
}

void main() {
    uint id = (pc.mode == 0u) ? vObjectID : uint(gl_PrimitiveID);
    outColor = vec4(palette[hash(id) % 24u], 1.0);
}
