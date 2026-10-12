#version 460
#extension GL_EXT_nonuniform_qualifier : require
#extension GL_EXT_scalar_block_layout : require
#extension GL_EXT_samplerless_texture_functions : require
#extension GL_GOOGLE_include_directive : require

// Draws the full-resolution openness over the frame (see occlusion_openness.glsl), in one
// of three outputs:
//
//   OUTPUT_OCCLUDED_SHARE  alpha = 1 - openness: the share of the indirect light to take
//                          away. Blended into the scene's alpha, where the opaque pass
//                          left each pixel's direct share, it leaves the share of the
//                          whole pixel to take away (see Renderer.encodeAmbientOcclusion).
//   OUTPUT_DARKENING       (0, 0, 0, 1): the second draw, whose blending does the work.
//   OUTPUT_OPENNESS        the openness as grey, for the debug view.
#include "occlusion_openness.glsl"

layout(push_constant, scalar) uniform PC {
    OpennessSource source;
    uint outputKind;
} pc;

const uint OUTPUT_OCCLUDED_SHARE = 0u;
const uint OUTPUT_DARKENING = 1u;
const uint OUTPUT_OPENNESS = 2u;

layout(location = 0) in vec2 vUV;
layout(location = 0) out vec4 outColor;

void main() {
    if (pc.outputKind == OUTPUT_DARKENING) {
        outColor = vec4(0.0, 0.0, 0.0, 1.0);
        return;
    }
    float openness = opennessAt(pc.source, ivec2(gl_FragCoord.xy));
    if (pc.outputKind == OUTPUT_OPENNESS) {
        outColor = vec4(vec3(openness), 1.0);
        return;
    }
    outColor = vec4(0.0, 0.0, 0.0, 1.0 - openness);
}
