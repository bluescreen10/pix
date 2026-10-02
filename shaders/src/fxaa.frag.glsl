#version 460
#extension GL_EXT_nonuniform_qualifier : require
#extension GL_EXT_scalar_block_layout : require
#extension GL_GOOGLE_include_directive : require

// fxaa smooths the finished frame's edges (see pix.AntiAliasingFXAA): Timothy Lottes'
// FXAA 3.11, its console variant — nine reads a pixel, the cheapest of the variants,
// chosen for low-end and mobile GPUs. It finds where brightness changes sharply, works
// out which way the edge runs, and blends the pixel with its neighbours along it, never
// across it.
//
// The source is the frame as it will be displayed, in an sRGB image, so a read returns
// linear light; edges are judged on display brightness, which is what the eye sees as
// a step, and is what the thresholds below are tuned for. The output is linear light
// for the sRGB target to encode.
#include "postfx.glsl"

layout(push_constant, scalar) uniform PC {
    POSTFX_ROOT
} pc;

layout(location = 0) out vec4 outColor;

// edgeThreshold is the contrast, relative to the brightest neighbour, an edge needs
// to be smoothed; edgeThresholdMin the least, so dark areas' noise is left alone.
// edgeSharpness bounds how far along a near-horizontal or near-vertical edge the
// second pair of reads reaches. All three are FXAA 3.11's console defaults.
const float edgeThreshold = 0.125;
const float edgeThresholdMin = 0.05;
const float edgeSharpness = 8.0;

vec3 readColor(vec2 uv) {
    return sampleImage(pc.source, pc.linearSampler, uv).rgb;
}

// displayLuma is the brightness of linear light as displayed, the square root standing
// in for the display's encoding curve.
float displayLuma(vec3 linear) {
    return sqrt(dot(linear, vec3(0.299, 0.587, 0.114)));
}

void main() {
    vec2 texel = pc.texelSize;
    vec3 colorM = readColor(vUV);
    float lumaM = displayLuma(colorM);

    // Each corner read lands between four pixels, so the bilinear filter averages them:
    // four reads cover the whole 3x3 neighbourhood. The tiny bias on the north-east
    // breaks the tie a perfectly diagonal edge would otherwise give.
    float lumaNW = displayLuma(readColor(vUV + vec2(-0.5, -0.5) * texel));
    float lumaNE = displayLuma(readColor(vUV + vec2(0.5, -0.5) * texel)) + 1.0 / 384.0;
    float lumaSW = displayLuma(readColor(vUV + vec2(-0.5, 0.5) * texel));
    float lumaSE = displayLuma(readColor(vUV + vec2(0.5, 0.5) * texel));

    float lumaMax = max(max(lumaNW, lumaSW), max(lumaNE, lumaSE));
    float lumaMin = min(min(lumaNW, lumaSW), min(lumaNE, lumaSE));
    float contrast = max(lumaMax, lumaM) - min(lumaMin, lumaM);
    if (contrast < max(edgeThresholdMin, lumaMax * edgeThreshold)) {
        outColor = vec4(colorM, 1.0);
        return;
    }

    // The direction brightness changes fastest, turned a quarter: the way the edge runs.
    vec2 direction = vec2(
        (lumaSW - lumaNE) + (lumaSE - lumaNW),
        (lumaSW - lumaNE) - (lumaSE - lumaNW));
    vec2 along = normalize(direction);

    // A short pair of reads along the edge, half a pixel each way...
    vec3 nearPair = readColor(vUV - along * texel * 0.5) + readColor(vUV + along * texel * 0.5);
    // ...and a longer pair, reaching further the closer the edge is to horizontal or
    // vertical, where a stair step is longest.
    vec2 alongFar = clamp(along / (min(abs(along.x), abs(along.y)) * edgeSharpness), -2.0, 2.0);
    vec3 farPair = readColor(vUV - alongFar * texel * 2.0) + readColor(vUV + alongFar * texel * 2.0);

    vec3 nearBlend = nearPair * 0.5;
    vec3 wideBlend = nearPair * 0.25 + farPair * 0.25;
    // The long reads can cross onto a different surface; outside the neighbourhood's own
    // range they did, and the short blend alone is used.
    float wideLuma = displayLuma(wideBlend);
    bool reachedTooFar = wideLuma < lumaMin || wideLuma > lumaMax;
    outColor = vec4(reachedTooFar ? nearBlend : wideBlend, 1.0);
}
