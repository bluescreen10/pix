// surface.glsl — the shading input a material's fragment shader builds and then
// shades, in one pass.
//
// This was the deferred-rendering SDK: Surface existed so a G-buffer fill and a
// separate lighting pass could agree on a layout without either knowing the other's
// material. With forward shading both halves live in the same shader, so what remains
// is just the struct that separates "resolve the material record" from "shade it" —
// still worth keeping apart, since one is per-material-type and the other is not.
#ifndef SURFACE_GLSL
#define SURFACE_GLSL

// Surface is the material-independent shading input: everything shading needs for a
// pixel, minus position.
//
// Emissive belongs here rather than being added after shading so the lit result and
// the emission are summed and sRGB-encoded ONCE — encoding them separately and
// blending is visibly wrong (srgb(a)+srgb(b) is far brighter than srgb(a+b); two 0.25
// terms already clip to white).
//
// `material` is a scratch block whose interpretation belongs to the shading model that
// filled it: metallic-roughness PBR packs (metallic, roughness, …); a Blinn-Phong
// model would pack (specular, shininess, …); a toon model a ramp index. `model` names
// which, so a shared helper can still tell them apart.
struct Surface {
    vec3 albedo;
    vec3 normal;   // world-space, normalized
    vec3 emissive;
    vec4 material; // model-defined; see above
    uint model;    // shading-model id
};

#endif
