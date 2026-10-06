#version 460
#extension GL_EXT_nonuniform_qualifier : require
#extension GL_EXT_scalar_block_layout : require
#extension GL_EXT_samplerless_texture_functions : require
#extension GL_GOOGLE_include_directive : require

// The HDR frame's last pass: expose the linear scene image and map its unbounded light
// into the 0..1 a display can show, compressing highlights instead of clipping them. It
// writes linear light; the target is sRGB and encodes it for display.
//
// It can also darken the scene by its ambient occlusion first, when nothing drew over
// the opaque scene since (see darkeningInToneMapping in ambient_occlusion.go): the same
// sum the darkening draws make with blending, but in the read this pass makes anyway.
#include "postfx.glsl"
#include "occlusion_openness.glsl"

layout(push_constant, scalar) uniform PC {
    POSTFX_ROOT
    uint toneMapOperator; // mirrors pix.ToneMapOperator
    float exposure;       // in stops: each +1 doubles the light before mapping
    // appliesOcclusion is 1 when the source is the opaque scene with its direct share in
    // alpha, to be darkened by the openness occlusion reads from.
    uint appliesOcclusion;
    float pad0;
    OpennessSource occlusion;
    float pad1;
} pc;

layout(location = 0) out vec4 outColor;

const uint TONEMAP_NEUTRAL = 0u;
const uint TONEMAP_ACES_FITTED = 1u;
const uint TONEMAP_REINHARD = 2u;
const uint TONEMAP_NONE = 3u;

// neutral is Khronos PBR Neutral: colours below the compression point pass almost
// untouched, so base colours survive; highlights roll off towards white.
// https://github.com/KhronosGroup/ToneMapping/tree/main/PBR_Neutral
vec3 neutral(vec3 c) {
    const float startCompression = 0.8 - 0.04;
    const float desaturation = 0.15;
    float x = min(c.r, min(c.g, c.b));
    float offset = x < 0.08 ? x - 6.25 * x * x : 0.04;
    c -= offset;
    float peak = max(c.r, max(c.g, c.b));
    if (peak < startCompression) {
        return c;
    }
    const float d = 1.0 - startCompression;
    float newPeak = 1.0 - d * d / (peak + d - startCompression);
    c *= newPeak / peak;
    float g = 1.0 - 1.0 / (desaturation * (peak - newPeak) + 1.0);
    return mix(c, vec3(newPeak), g);
}

// acesFitted is Stephen Hill's fit of the ACES reference rendering and output transforms, for
// an sRGB display. The matrices take linear sRGB into the transform's working space and
// back.
vec3 acesFitted(vec3 c) {
    const mat3 toACES = mat3(
        vec3(0.59719, 0.07600, 0.02840),
        vec3(0.35458, 0.90834, 0.13383),
        vec3(0.04823, 0.01566, 0.83777));
    const mat3 fromACES = mat3(
        vec3(1.60475, -0.10208, -0.00327),
        vec3(-0.53108, 1.10813, -0.07276),
        vec3(-0.07367, -0.00605, 1.07602));
    vec3 v = toACES * c;
    vec3 a = v * (v + 0.0245786) - 0.000090537;
    vec3 b = v * (0.983729 * v + 0.4329510) + 0.238081;
    return clamp(fromACES * (a / b), 0.0, 1.0);
}

void main() {
    vec4 scene = sampleImage(pc.source, pc.linearSampler, vUV);
    vec3 c = scene.rgb;
    if (pc.appliesOcclusion != 0u) {
        // The occluded share of the indirect light, which is all of the colour but its
        // direct share (see outputAlpha in material_common.glsl).
        float occludedShare = 1.0 - opennessAt(pc.occlusion, ivec2(gl_FragCoord.xy));
        c *= 1.0 - occludedShare * (1.0 - scene.a);
    }
    c *= exp2(pc.exposure);
    if (pc.toneMapOperator == TONEMAP_ACES_FITTED) {
        c = acesFitted(c);
    } else if (pc.toneMapOperator == TONEMAP_REINHARD) {
        c = c / (1.0 + c);
    } else if (pc.toneMapOperator != TONEMAP_NONE) {
        c = neutral(c);
    }
    outColor = vec4(c, 1.0);
}
