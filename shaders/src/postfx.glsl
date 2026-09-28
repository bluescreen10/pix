// postfx.glsl — what every post-processing shader shares: the root the renderer pushes,
// the full-screen varying, and sampling helpers.
//
// A post-processing root starts with POSTFX_ROOT, and a step's own parameters follow it:
//
//     layout(push_constant, scalar) uniform PC {
//         POSTFX_ROOT
//         float strength; // ShaderStep.Params start here
//     } pc;
//
// Colours are linear and unclamped throughout: the scene is HDR, and nothing encodes for
// display until the frame's last pass.
#ifndef PIX_POSTFX_GLSL
#define PIX_POSTFX_GLSL

#include "bindless.glsl"

// source is the bindless index of the image to read — the scene, or the previous step's
// result — and linearSampler a linear, clamp-to-edge sampler. texelSize is 1/size of
// source; time is seconds since the scene's clock started.
//
// A shader including this declares, before it, the extensions it relies on:
// GL_EXT_nonuniform_qualifier and GL_EXT_scalar_block_layout.
#define POSTFX_ROOT         \
    uint source;            \
    uint linearSampler;     \
    vec2 texelSize;         \
    float time;             \
    float postfxPad0;       \
    float postfxPad1;       \
    float postfxPad2;

layout(location = 0) in vec2 vUV;

// sampleImage reads one bindless image with one bindless sampler.
vec4 sampleImage(uint image, uint samp, vec2 uv) {
    return texture(sampler2D(gTextures[nonuniformEXT(image)], gSamplers[nonuniformEXT(samp)]), uv);
}

#endif
