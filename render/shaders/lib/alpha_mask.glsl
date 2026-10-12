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

#include <pix/bindless.glsl>

// UV1 and UV2 are the values of a UVSet (materials.UV1, materials.UV2).
const uint UV1 = 0u;
const uint UV2 = 1u;

// AlphaMask is how a material cuts its surface out, matching materials.AlphaMask: where
// its map's alpha at the coordinates of uvSet, times alpha, falls below cutoff. A cut-off
// of 0 keeps every texel.
struct AlphaMask {
    uint map;
    uint mapSampler;
    uint uvSet;
    float alpha;
    float cutoff;
};

// TransmissionMask is how much light a material lets through, matching
// materials.TransmissionMask: factor, times its map's red at the coordinates of uvSet. A
// factor of 0 lets none through.
struct TransmissionMask {
    uint map;
    uint mapSampler;
    uint uvSet;
    float factor;
};

// UVTransform matches materials.UVTransform: coordinates are scaled, then offset.
struct UVTransform {
    vec2 scale;
    vec2 offset;
};

vec2 transformUV(UVTransform transform, vec2 uv) {
    return uv * transform.scale + transform.offset;
}

// Mask is one entry of a material pool's mask table, one per record slot, matching
// maskEntry in materials/pool.go: 80 bytes. uv1 and uv2 are the material's transforms of
// the mesh's two sets of texture coordinates, which both masks' maps are read through.
struct Mask {
    AlphaMask alpha;
    TransmissionMask transmission;
    UVTransform uv1;
    UVTransform uv2;
    uint pad0;
    uint pad1;
    uint pad2;
};
layout(buffer_reference, scalar) readonly buffer MaskBuf { Mask v[]; };

// maskUV is the coordinates a mask's map, reading the set uvSet, is read at: the
// fragment's uv1 or uv2, moved by the entry's transform of that set.
vec2 maskUV(Mask entry, uint uvSet, vec2 uv1, vec2 uv2) {
    return uvSet == UV2 ? transformUV(entry.uv2, uv2) : transformUV(entry.uv1, uv1);
}

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
//
// TODO: partial or coloured shadows. All or nothing per texel, glass letting through 30%
// shadows fully and 70% not at all; dithered shadows or a colour shadow map would grade
// them.
bool letsLightThrough(TransmissionMask mask, vec2 uv) {
    float transmission = mask.factor;
    if (mask.map != NO_ALPHA_MASK_MAP) {
        transmission *= texture(sampler2D(gTextures[nonuniformEXT(mask.map)], gSamplers[nonuniformEXT(mask.mapSampler)]), uv).r;
    }
    return transmission >= 0.5;
}

#endif
