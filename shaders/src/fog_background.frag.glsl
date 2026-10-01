#version 460
#extension GL_EXT_nonuniform_qualifier : require
#extension GL_EXT_scalar_block_layout : require
#extension GL_GOOGLE_include_directive : require

// fog_background — fogs the pixels no geometry covers, which no lit shader runs for and
// so no applyFog reaches: the fog out to the volume's reach, from its last slice, to
// end up as background * transmittance + light, which is what applyFog gives a
// surface. It gets there through ordinary "over" blending — src * a + dst * (1 - a) —
// with a = 1 - transmittance and src = light / a, since that is the blend every
// backend has (see Renderer.encodeFogBackground).
#include "postfx.glsl"

layout(push_constant, scalar) uniform PC {
    POSTFX_ROOT
    uint volume;
    float lastSlice; // the last slice's depth coordinate in the volume
    uint pad0;
    uint pad1;
} pc;

layout(location = 0) out vec4 outColor;

void main() {
    // Depth is read by texel: a depth format need not support filtering.
    if (texelFetch(sampler2D(gTextures[nonuniformEXT(pc.sceneDepth)], gSamplers[nonuniformEXT(pc.linearSampler)]), ivec2(gl_FragCoord.xy), 0).r > 0.0) {
        discard; // geometry: its own shader fogged it
    }
    vec2 uv = gl_FragCoord.xy * pc.texelSize;
    vec4 fog = textureLod(sampler3D(gTextures3D[nonuniformEXT(pc.volume)], gSamplers[nonuniformEXT(pc.linearSampler)]), vec3(uv, pc.lastSlice), 0.0);
    // Where the fog lets nearly everything through, it adds nearly nothing either, so
    // the floor on the coverage changes no visible result.
    float coverage = 1.0 - fog.a;
    outColor = vec4(fog.rgb / max(coverage, 1e-4), coverage);
}
