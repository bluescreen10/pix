// Shared palette + shuffle for the id debug views (object and triangle).
#ifndef SCENE_DEBUG_PALETTE_GLSL
#define SCENE_DEBUG_PALETTE_GLSL

// A small fixed palette rather than a raw hash-to-RGB, which tends to come out muddy
// and oversaturated.
const vec3 debugPalette[24] = vec3[](
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
// sequential ids cycle through the palette in visible bands (every 24 repeating the
// same run of colours) instead of looking evenly shuffled.
uint debugHash(uint x) {
    x ^= x >> 16;
    x *= 0x7feb352du;
    x ^= x >> 15;
    x *= 0x846ca68bu;
    x ^= x >> 16;
    return x;
}

vec4 debugIDColor(uint id) {
    return vec4(debugPalette[debugHash(id) % 24u], 1.0);
}

#endif
