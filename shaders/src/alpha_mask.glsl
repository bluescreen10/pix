// alpha_mask.glsl — what a material cuts out of the passes that draw it: its surface,
// by alpha (see materials.Masked), and from the shadows, its texels that let light
// through (see materials.Transmissive). The material's own fragment shader and the
// depth-only passes read the same mask, so that what a shadow or the depth prepass
// leaves out of a surface is exactly what shading leaves out.
//
// A shader including this declares, before it, the extensions it relies on:
// GL_EXT_buffer_reference, GL_EXT_scalar_block_layout and GL_EXT_nonuniform_qualifier.
#ifndef PIX_ALPHA_MASK_GLSL
#define PIX_ALPHA_MASK_GLSL

#include "bindless.glsl"

// AlphaMask is how a material cuts its surface out, matching materials.AlphaMask. A
// cut-off of 0 keeps every texel.
struct AlphaMask {
    uint map;
    uint mapSampler;
    float alpha;
    float cutoff;
};

// TransmissionMask is how much light a material lets through, matching
// materials.TransmissionMask. A factor of 0 lets none through.
struct TransmissionMask {
    uint map;
    uint mapSampler;
    float factor;
    uint pad;
};

// Mask is one entry of a material pool's mask table, one per record slot, matching
// maskEntryOf in materials/pool.go.
struct Mask {
    AlphaMask alpha;
    TransmissionMask transmission;
};
layout(buffer_reference, scalar) readonly buffer MaskBuf { Mask v[]; };

// NO_ALPHA_MASK_MAP is the map index of a mask with no map (materials.NoTextureIndex):
// its factor alone decides.
const uint NO_ALPHA_MASK_MAP = 0xFFFFFFFFu;

// isCutOut reports whether the surface mask describes has none at uv: its map's alpha,
// times its own, falls below its cut-off.
bool isCutOut(AlphaMask mask, vec2 uv) {
    float alpha = mask.alpha;
    if (mask.map != NO_ALPHA_MASK_MAP) {
        alpha *= texture(sampler2D(gTextures[nonuniformEXT(mask.map)], gSamplers[nonuniformEXT(mask.mapSampler)]), uv).a;
    }
    return alpha < mask.cutoff;
}

// letsLightThrough reports whether the surface mask describes lets through at least half
// the light reaching it at uv: its factor, times its map's red, where KHR_materials_
// transmission keeps it. A shadow leaves such a texel out.
bool letsLightThrough(TransmissionMask mask, vec2 uv) {
    float transmission = mask.factor;
    if (mask.map != NO_ALPHA_MASK_MAP) {
        transmission *= texture(sampler2D(gTextures[nonuniformEXT(mask.map)], gSamplers[nonuniformEXT(mask.mapSampler)]), uv).r;
    }
    return transmission >= 0.5;
}

#endif
