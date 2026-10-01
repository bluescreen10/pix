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
// The same heap again for 3D textures. A texture keeps its index whichever
// declaration reads it; a shader uses the one matching the texture's kind.
layout(set = 0, binding = 0) uniform texture3D gTextures3D[];
layout(set = 0, binding = 2) uniform sampler gSamplers[];

// Binding 1: storage images, what compute shaders write, declared again for each kind
// as binding 0 is. A writable texture's index is the same here as in gTextures and
// gTextures3D. Declared without a format, so one array serves every format — which
// makes them write-only: read one back by sampling it.
layout(set = 0, binding = 1) uniform writeonly image2D gImages[];
layout(set = 0, binding = 1) uniform writeonly image3D gImages3D[];

#endif
