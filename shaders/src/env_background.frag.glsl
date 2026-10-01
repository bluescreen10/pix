#version 460
#extension GL_EXT_nonuniform_qualifier : require
#extension GL_EXT_scalar_block_layout : require
#extension GL_GOOGLE_include_directive : require

// env_background — draws a scene's environment behind it: the image itself, in the
// direction each pixel looks, wherever no geometry was drawn. The pass's depth test
// keeps it to those pixels (see Renderer.encodeEnvironmentBackground).
#include "postfx.glsl"
#include "environment.glsl"

layout(push_constant, scalar) uniform PC {
    POSTFX_ROOT
    mat4 inverseViewProj;
    uint environment;
    float intensity;
    float rotation;
    uint pad0;
} pc;

layout(location = 0) out vec4 outColor;

void main() {
    vec2 ndc = gl_FragCoord.xy * pc.texelSize * 2.0 - 1.0;
    vec4 nearPoint = pc.inverseViewProj * vec4(ndc, 1.0, 1.0); // reversed depth: 1 is the near plane
    vec4 midPoint = pc.inverseViewProj * vec4(ndc, 0.5, 1.0);
    vec3 ray = normalize(midPoint.xyz / midPoint.w - nearPoint.xyz / nearPoint.w);
    vec2 uv = equirectUV(unrotateEnvironment(ray, pc.rotation));
    outColor = vec4(textureLod(sampler2D(gTextures[nonuniformEXT(pc.environment)], gSamplers[nonuniformEXT(pc.linearSampler)]), uv, 0.0).rgb * pc.intensity, 1.0);
}
