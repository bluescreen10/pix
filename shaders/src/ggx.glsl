// ggx.glsl — GGX microfacet terms shared by direct shading (scene_pbr.frag.glsl) and the
// split-sum table environment reflections are weighted by (env_brdf.comp.glsl), so that
// light from a lamp and light from the environment meet the same surface.
#ifndef PIX_GGX_GLSL
#define PIX_GGX_GLSL

// smithVisibility is the height-correlated Smith masking-shadowing term of GGX, folded
// with the specular term's 1 / (4 NdotV NdotL): what the distribution and Fresnel are
// multiplied by. alpha is GGX's alpha, the perceptual roughness squared.
//
// Height-correlated: a microfacet hidden from the eye is likelier hidden from the light
// too, where they look from nearby directions. Multiplying the two directions' masking
// as if they were independent darkens rough surfaces more than they are.
//
// This is Hammon's approximation of it, linear in alpha between its two ends, rather
// than the exact form's two square roots. It stays within a few percent of the exact
// term and, measured on the beach scene's one light, took 0.05 ms off its opaque pass
// (2.09 -> 2.04 ms, examples/beachbench at 2500x1400, MSAA 4x); a fragment lit by more
// lights saves that much per light.
float smithVisibility(float NdotV, float NdotL, float alpha) {
    return 0.5 / max(mix(2.0 * NdotL * NdotV, NdotL + NdotV, alpha), 1e-5);
}

#endif // PIX_GGX_GLSL
