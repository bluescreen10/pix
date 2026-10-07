// material_common.glsl — shared by the scene material fragment shaders (forward AND
// G-buffer fill). It pulls in the bindless heap + light table (lighting.glsl), then
// declares the DrawRoot push constant (materials is an opaque address — each shader
// casts it to its own per-type record buffer) and the vertex→fragment varyings.
//
// It deliberately does NOT declare a color output: forward shaders write a single
// outColor, the G-buffer fill writes several MRT targets, so each fragment shader
// declares its own outputs.
#ifndef MATERIAL_COMMON_GLSL
#define MATERIAL_COMMON_GLSL

// Bindless heap, light table, shadow sampling and fog.
#include "lighting.glsl"
#include "alpha_mask.glsl"

const uint MAT_COLOR_MAP = 1u;
const uint FLAG_RECEIVES_SHADOW = 4u; // Drawable flag (mirrors pix.DrawableReceivesShadow)

// DrawRoot mirrors pix.drawRoot. Buffer pointers are opaque uint64 (the vertex stage
// uses pos/attr/descs/models/drawables/visible; the fragment stage casts `materials`
// to its own per-type record buffer and reads `lights`). There is no regionBase:
// each indirect command sets firstInstance, so gl_InstanceIndex indexes visible[].
// Pushed inline rather than behind a device address: it fits in push constants on
// every backend, so the shader reads its parameters directly instead of chasing a
// pointer to reach them. Fields that are themselves addresses stay addresses — those
// point at unbounded arrays, so that indirection is inherent.
layout(push_constant, scalar) uniform PC {
    mat4 viewProj;
    uint64_t pos;
    uint64_t attr;
    uint64_t descs;
    uint64_t models;
    uint64_t drawables;
    uint64_t visible;
    uint64_t materials;
    LightBuf lights;
    vec4 eye;
    uint shadowSampler; // bindless index of the PCF comparison sampler
    // time is elapsed seconds since the scene's clock started (Scene.clockStart),
    // passed to every vertex/fragment shader pair unconditionally — read it or not.
    float time;
    // directShareInAlpha is set in the opaque pass while ambient occlusion is on: a
    // material then writes outputAlpha's direct share where its alpha would go.
    uint directShareInAlpha;
    uint spad1;
    // masks is the material pool's mask table (see alpha_mask.glsl), for a batch of
    // masked materials, and 0 for any other.
    uint64_t masks;
    // sceneCopy is the heap index of the opaque scene, copied before the transparent
    // pass (see pix.sceneCopy), in that pass, and NO_SCENE_COPY elsewhere;
    // sceneCopySampler is the sampler it is read with.
    uint sceneCopy;
    uint sceneCopySampler;
} pc;

// NO_SCENE_COPY is pc.sceneCopy when there is no scene copy to read (pix.noSceneCopy).
const uint NO_SCENE_COPY = 0xFFFFFFFFu;

// Vertex → fragment varyings (produced by scene_draw.vert).
layout(location = 0) in vec3 vColor;
layout(location = 1) in vec2 vUV;
layout(location = 2) flat in uint vMat;
layout(location = 3) in vec3 vWorldPos;
layout(location = 4) in vec3 vNormal;
layout(location = 5) flat in uint vFlags; // drawable flags (FLAG_RECEIVES_SHADOW, …)

// sampleBase returns base color × vertex color, modulated by the color map when the
// MAT_COLOR_MAP flag is set. Fields come from the shader's own material record.
vec4 sampleBase(vec4 base, uint flags, uint colorMap, uint samp) {
    vec4 c = base * vec4(vColor, 1.0);
    if ((flags & MAT_COLOR_MAP) != 0u) {
        c *= texture(sampler2D(gTextures[nonuniformEXT(colorMap)], gSamplers[nonuniformEXT(samp)]), vUV);
    }
    return c;
}

// discardCutOut discards the fragment where a masked material has no surface (see
// materials.Masked). Every material that can be masked calls it first thing; it costs
// any other a branch, as pc.masks is 0 outside batches of masked materials.
void discardCutOut() {
    if (pc.masks == 0ul) {
        return;
    }
    if (isCutOut(MaskBuf(pc.masks).v[vMat].alpha, vUV)) {
        discard;
    }
}

// outputAlpha is the alpha a material writes beside its finished colour. Ordinarily
// that is its alpha. In the opaque pass with ambient occlusion on — pc.directShareInAlpha
// — it is instead the share of the colour that occlusion must leave alone: all of it but
// what indirect light contributes, which the occlusion pass then takes its share of.
// color is the finished colour, fogged; lit the same before fog, and indirect the part
// of lit that ambient and environment light make up.
//
// A material that writes its alpha without this is not occluded, as long as that alpha
// is 1, which an opaque surface's usually is.
float outputAlpha(vec3 color, vec3 lit, vec3 indirect, float alpha) {
    if (pc.directShareInAlpha == 0u) {
        return alpha;
    }
    // Fog scales the colour it covers and adds its own, so what the indirect light
    // contributes to the fogged colour is the difference fogging it with and without
    // that light makes.
    vec3 withoutIndirect = applyFog(lit - indirect, vWorldPos, pc.eye.xyz, pc.lights.fogColor, pc.lights.fogParams);
    const vec3 luma = vec3(0.2126, 0.7152, 0.0722);
    float total = dot(color, luma);
    if (total <= 0.0) {
        return 1.0;
    }
    return 1.0 - clamp(dot(color - withoutIndirect, luma) / total, 0.0, 1.0);
}

#endif // MATERIAL_COMMON_GLSL
