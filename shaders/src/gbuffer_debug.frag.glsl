// gbuffer_debug.frag.glsl — a fullscreen pass that shows ONE G-buffer target instead
// of the shaded result, for answering "is the geometry pass writing what I think?"
// without a graphics debugger.
//
// It reuses the deferred lighting pass's root struct verbatim (every target, the
// sampler and invViewProj are already there), so it is a second fragment shader over
// the same data rather than any new plumbing. pc.root.debugView picks the target.
#version 460
#extension GL_EXT_buffer_reference : require
#extension GL_EXT_buffer_reference2 : require
#extension GL_EXT_scalar_block_layout : require
#extension GL_EXT_nonuniform_qualifier : require
#extension GL_EXT_shader_explicit_arithmetic_types_int64 : require
#extension GL_GOOGLE_include_directive : require

#include "lighting.glsl"
#include "gbuffer.glsl"

// Mirrors pix.lightingRoot; debugView mirrors pix.DebugView.
layout(buffer_reference, scalar) readonly buffer LightingRoot {
    mat4 invViewProj;
    vec4 eye;
    LightBuf lights;
    uint shadowSampler;
    uint gbufferSampler;
    uint diffuseTexture;
    uint normalTexture;
    uint materialTexture;
    uint emissiveTexture;
    uint depthTexture;
    vec2 screen;
    uint debugView;
};
layout(push_constant) uniform PC { LightingRoot root; } pc;

const uint VIEW_ALBEDO = 1u;
const uint VIEW_NORMAL = 2u;
const uint VIEW_MATERIAL = 3u;
const uint VIEW_EMISSIVE = 4u;
const uint VIEW_DEPTH = 5u;
const uint VIEW_POSITION = 6u;

layout(location = 0) out vec4 outColor;

vec4 fetch(uint tex, vec2 uv) {
    return texture(sampler2D(gTextures[nonuniformEXT(tex)],
                             gSamplers[nonuniformEXT(pc.root.gbufferSampler)]), uv);
}

void main() {
    vec2 uv = gl_FragCoord.xy / pc.root.screen;
    vec3 c;

    switch (pc.root.debugView) {
    case VIEW_NORMAL: {
        // Octahedral-encoded, so decode before display, then remap [-1,1] to [0,1] —
        // the familiar pastel normal map. Raw RG would show only two channels of it.
        vec3 n = decodeOct(fetch(pc.root.normalTexture, uv).rg);
        c = n * 0.5 + 0.5;
        break;
    }
    case VIEW_MATERIAL: {
        // Metallic/roughness/occlusion live in separate channels; showing them as RGB
        // makes each one readable on its own.
        c = fetch(pc.root.materialTexture, uv).rgb;
        break;
    }
    case VIEW_EMISSIVE:
        c = linearToSrgb(fetch(pc.root.emissiveTexture, uv).rgb);
        break;
    case VIEW_DEPTH: {
        // Reversed-Z: 1 is the near plane and 0 is the far one, so invert to get the
        // conventional "near is dark" ramp. The raw values crowd against 1 near the
        // camera, so a sqrt spreads the useful range out.
        float d = fetch(pc.root.depthTexture, uv).r;
        c = vec3(sqrt(1.0 - d));
        break;
    }
    case VIEW_POSITION: {
        // World position reconstructed from depth — the fractional part, so the scene
        // reads as a 1-unit grid and any discontinuity in the reconstruction shows up.
        float d = fetch(pc.root.depthTexture, uv).r;
        vec3 world = worldFromDepth(gl_FragCoord.xy, pc.root.screen, d, pc.root.invViewProj);
        c = fract(world);
        break;
    }
    default: // VIEW_ALBEDO
        c = fetch(pc.root.diffuseTexture, uv).rgb;
        break;
    }
    outColor = vec4(c, 1.0);
}
