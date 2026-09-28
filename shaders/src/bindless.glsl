// bindless.glsl — the bindless heap every shader samples through. Textures and samplers
// are addressed by index; the renderer pushes the indices a shader needs in its root.
#ifndef PIX_BINDLESS_GLSL
#define PIX_BINDLESS_GLSL

// Set 0: sampled images at binding 0, samplers at binding 2.
layout(set = 0, binding = 0) uniform texture2D gTextures[];
// The same heap aliased under a second name, for comparison (shadow) sampling. Without
// it SPIRV-Cross promotes every gTextures sample to depth2d's scalar return type on
// Metal, losing the green and blue channels of every colour texture.
layout(set = 0, binding = 0) uniform texture2D gShadowTextures[];
layout(set = 0, binding = 2) uniform sampler gSamplers[];

#endif
