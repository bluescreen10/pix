// alpha_mask.glsl — how a masked material cuts its surface out (see materials.Masked),
// shared by the material's own fragment shader and the depth-only passes, which read
// the same mask so that what a shadow or the depth prepass leaves out is exactly what
// shading leaves out.
//
// A shader including this declares, before it, the extensions it relies on:
// GL_EXT_buffer_reference, GL_EXT_scalar_block_layout and GL_EXT_nonuniform_qualifier.
#ifndef PIX_ALPHA_MASK_GLSL
#define PIX_ALPHA_MASK_GLSL

#include "bindless.glsl"

// AlphaMask is one entry of a material pool's mask table, one per record slot, matching
// alphaMaskOf in materials/material.go. A cut-off of 0 keeps every texel.
struct AlphaMask {
    uint map;
    uint mapSampler;
    float alpha;
    float cutoff;
};
layout(buffer_reference, scalar) readonly buffer AlphaMaskBuf { AlphaMask v[]; };

// NO_ALPHA_MASK_MAP is the map index of a mask with no map (materials.NoTextureIndex):
// alpha alone decides.
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

#endif
