// spherical_harmonics.glsl — the first three bands (nine terms) of the real spherical
// harmonics, which an environment's diffuse light is stored in (see env_irradiance.comp
// and environmentDiffuse in lighting.glsl). The projection and the evaluation must use
// the same basis, so both read it from here.
#ifndef PIX_SPHERICAL_HARMONICS_GLSL
#define PIX_SPHERICAL_HARMONICS_GLSL

// shBasis is the nine basis functions at unit direction d, band by band: Y00; Y1-1,
// Y10, Y11; Y2-2, Y2-1, Y20, Y21, Y22.
float[9] shBasis(vec3 d) {
    return float[9](
        0.282095,
        0.488603 * d.y,
        0.488603 * d.z,
        0.488603 * d.x,
        1.092548 * d.x * d.y,
        1.092548 * d.y * d.z,
        0.315392 * (3.0 * d.z * d.z - 1.0),
        1.092548 * d.x * d.z,
        0.546274 * (d.x * d.x - d.y * d.y));
}

// shBandScale is what coefficient k of the light arriving from every direction is
// scaled by to become coefficient k of the light a white diffuse surface reflects:
// the cosine lobe's convolution of its band — pi, 2 pi / 3 and pi / 4 for bands 0, 1
// and 2 — over the pi a Lambertian surface divides by. A uniform white environment
// then lights a white surface to exactly 1.
float shBandScale(uint k) {
    if (k == 0u) {
        return 1.0;
    }
    if (k < 4u) {
        return 2.0 / 3.0;
    }
    return 0.25;
}

#endif // PIX_SPHERICAL_HARMONICS_GLSL
