#version 460
#extension GL_EXT_buffer_reference : require
#extension GL_EXT_buffer_reference2 : require
#extension GL_EXT_scalar_block_layout : require
#extension GL_EXT_nonuniform_qualifier : require
#extension GL_EXT_shader_explicit_arithmetic_types_int64 : require
#extension GL_GOOGLE_include_directive : require

// Unlit particle shading: base color × the particle's own color/alpha × color map,
// plus emissive, fogged — the particle-path analogue of scene_basic.frag.glsl. Reads
// the SAME 48-byte Material record layout BasicMaterial writes (see basic.go's
// Bytes doc comment): NewParticleContainer requires config.Material to be a
// *materials.BasicMaterial precisely so this record read is guaranteed safe, not
// assumed (see particle.go).
#include "particle_common.glsl"

layout(location = 0) out vec4 outColor;

struct Material {
    vec4 color;
    vec4 emissive;
    uint colorMap;
    uint samp;
    uint flags;
    uint pad;
};
layout(buffer_reference, scalar) readonly buffer MatBuf { Material v[]; };

void main() {
    Material m = MatBuf(pc.materials).v[pc.materialID];
    vec4 base = sampleParticleBase(m.color, m.flags, m.colorMap, m.samp);
    // Unlit, but still fogged: an unlit particle that ignored fog would hang in
    // front of the haze while everything around it receded into it.
    vec3 c = applyFog(base.rgb + m.emissive.rgb, vWorldPos, pc.eye.xyz, pc.lights.fogColor, pc.lights.fogParams);
    outColor = vec4(linearToSrgb(c), base.a);
}
