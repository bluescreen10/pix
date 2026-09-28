#version 460
#extension GL_EXT_nonuniform_qualifier : require
#extension GL_EXT_scalar_block_layout : require
#extension GL_GOOGLE_include_directive : require

// Bloom, downsample: one level of the chain from the level above it (or the scene), with
// the 13-tap filter from Jimenez, "Next Generation Post Processing in Call of Duty:
// Advanced Warfare". Thirteen bilinear taps cover a 6x6 texel footprint as five
// overlapping 2x2 boxes, which is what keeps the chain from shimmering as the camera moves.
#include "postfx.glsl"

layout(push_constant, scalar) uniform PC {
    POSTFX_ROOT
    uint firstLevel; // 1 when reading the scene itself
    uint pad0;
    uint pad1;
    uint pad2;
} pc;

layout(location = 0) out vec4 outColor;

// tap reads the source offset by whole source texels.
vec3 tap(vec2 offset) {
    return sampleImage(pc.source, pc.linearSampler, vUV + offset * pc.texelSize).rgb;
}

// karisWeight is 1 / (1 + luminance): the brighter a box, the less it counts.
float karisWeight(vec3 c) {
    return 1.0 / (1.0 + dot(c, vec3(0.2126, 0.7152, 0.0722)));
}

void main() {
    vec3 a = tap(vec2(-2.0, 2.0));
    vec3 b = tap(vec2(0.0, 2.0));
    vec3 c = tap(vec2(2.0, 2.0));
    vec3 d = tap(vec2(-2.0, 0.0));
    vec3 e = tap(vec2(0.0, 0.0));
    vec3 f = tap(vec2(2.0, 0.0));
    vec3 g = tap(vec2(-2.0, -2.0));
    vec3 h = tap(vec2(0.0, -2.0));
    vec3 i = tap(vec2(2.0, -2.0));
    vec3 j = tap(vec2(-1.0, 1.0));
    vec3 k = tap(vec2(1.0, 1.0));
    vec3 l = tap(vec2(-1.0, -1.0));
    vec3 m = tap(vec2(1.0, -1.0));

    // The five boxes: the centre one weighs half, the four corner ones an eighth each.
    vec3 centre = (j + k + l + m) * 0.25;
    vec3 topLeft = (a + b + d + e) * 0.25;
    vec3 topRight = (b + c + e + f) * 0.25;
    vec3 bottomLeft = (d + e + g + h) * 0.25;
    vec3 bottomRight = (e + f + h + i) * 0.25;

    vec3 color;
    if (pc.firstLevel != 0u) {
        // Karis average on the first level only: weighting each box by its inverse
        // luminance stops a single blazing pixel — a specular spark — from blooming into
        // a blob that flickers as it moves. Later levels are already smooth.
        float wc = karisWeight(centre) * 0.5;
        float w1 = karisWeight(topLeft) * 0.125;
        float w2 = karisWeight(topRight) * 0.125;
        float w3 = karisWeight(bottomLeft) * 0.125;
        float w4 = karisWeight(bottomRight) * 0.125;
        color = (centre * wc + topLeft * w1 + topRight * w2 + bottomLeft * w3 + bottomRight * w4) / (wc + w1 + w2 + w3 + w4);
    } else {
        color = centre * 0.5 + (topLeft + topRight + bottomLeft + bottomRight) * 0.125;
    }
    outColor = vec4(max(color, vec3(0.0)), 1.0);
}
