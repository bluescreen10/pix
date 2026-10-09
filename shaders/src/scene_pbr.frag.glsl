// scene_pbr.frag.glsl — PBRMaterial's fragment shader: surface + lighting in one pass.
//
// It resolves a Surface from the material record (materialSurface), then shades that
// Surface against the light table (shadeSurface). The split is not structural any more
// — both halves run here, back to back — but it keeps the per-material-type half
// separate from the half every shading model shares.
//
// The glass path is a specialization constant, TRANSMISSION, compiled out of every
// pipeline whose draws let no light through: present but never taken, it cost the beach
// scene's opaque pass (examples/beachbench, 2500x1400, MSAA 4x) 0.37 ms of 2.6.
// TODO: profile the per-map flags. They stay runtime tests, as a constant per map would
// split the batches that draw many materials at once.
#version 460
#extension GL_EXT_buffer_reference : require
#extension GL_EXT_buffer_reference2 : require
#extension GL_EXT_scalar_block_layout : require
#extension GL_EXT_nonuniform_qualifier : require
#extension GL_EXT_shader_explicit_arithmetic_types_int64 : require
#extension GL_GOOGLE_include_directive : require

// Geometry passes: bindless heap + light table + the DrawRoot push constant and the
// vertex-pull varyings.
#include "material_common.glsl"
#include "surface.glsl"
#include "ggx.glsl"

// TRANSMISSION keeps the glass path (see main): the renderer sets it false for draws
// whose materials let no light through (see pix.fragmentVariant), which then shade
// every light without its diffuse and reflection scales, and never read the scene copy.
// IDs below 16 are the renderer's (see material_common.glsl).
layout(constant_id = 2) const bool TRANSMISSION = true;

// The optional parts of this shader, as FEATURES bits (materials.FeatureClearcoat and
// those beside it). A part the pipeline's materials do not use is compiled out.
const uint FEATURE_CLEARCOAT = 1u;
const uint FEATURE_SHEEN = 2u;
const uint FEATURE_ANISOTROPY = 4u;
const bool CLEARCOAT = (FEATURES & FEATURE_CLEARCOAT) != 0u;
const bool SHEEN = (FEATURES & FEATURE_SHEEN) != 0u;
const bool ANISOTROPY = (FEATURES & FEATURE_ANISOTROPY) != 0u;

// This model's id and its interpretation of Surface.material (the model-defined
// G-buffer slot). Every pass goes through these two helpers, so the packing is stated
// once: another shading model packs its own parameters into the same channels.
const uint MODEL_PBR = 0u;

vec4 pbrPack(float metallic, float roughness, float occlusion) {
    return vec4(metallic, roughness, occlusion, 0.0);
}

float pbrMetallic(Surface s) { return s.material.r; }
float pbrRoughness(Surface s) { return s.material.g; }
// pbrOcclusion is how much of the light around it reaches the surface, by its occlusion
// map: what ambient and environment light are scaled by.
float pbrOcclusion(Surface s) { return s.material.b; }
// pbrF0 is how much of the light meeting it head-on the surface reflects, in colour:
// its dielectric reflectance, or where it is metal, its albedo.
vec3 pbrF0(Surface s) {
    return mix(s.specular.rgb, s.albedo, pbrMetallic(s));
}

// pbrF90 is how much of the light meeting it at a grazing angle the surface reflects:
// all of it, unless a dielectric's specular intensity lowers it.
float pbrF90(Surface s) {
    return mix(s.specular.a, 1.0, pbrMetallic(s));
}

// ---------------------------------------------------------------------------
// Outputs
// ---------------------------------------------------------------------------
layout(location = 0) out vec4 outColor;


// ---------------------------------------------------------------------------
// Surface from the material record (forward + deferred)
// ---------------------------------------------------------------------------

const uint MAT_NORMAL_MAP = 2u;
const uint MAT_METAL_MAP = 4u;
const uint MAT_ROUGH_MAP = 8u;
const uint MAT_TRANS_MAP = 16u;
const uint MAT_OCCLUSION_MAP = 32u;
const uint MAT_EMISSIVE_MAP = 64u;
const uint MAT_IGNORES_ALPHA = 128u;
const uint MAT_SPECULAR_INTENSITY_MAP = 256u;
const uint MAT_SPECULAR_COLOR_MAP = 512u;
const uint MAT_CLEARCOAT_MAP = 1024u;
const uint MAT_CLEARCOAT_ROUGHNESS_MAP = 2048u;
const uint MAT_CLEARCOAT_NORMAL_MAP = 4096u;
const uint MAT_SHEEN_COLOR_MAP = 8192u;
const uint MAT_SHEEN_ROUGHNESS_MAP = 16384u;
const uint MAT_ANISOTROPY_MAP = 32768u;

// Material mirrors materials.PBRMaterial's record (284 bytes). Each map carries its own
// sampler.
struct Material {
    vec4 color;
    vec4 emissive;
    float metallic;
    float roughness;
    float transmission;
    uint flags;
    uint colorMap;
    uint colorSampler;
    uint normalMap;
    uint normalSampler;
    uint metalMap;
    uint metalSampler;
    uint roughMap;
    uint roughSampler;
    uint transMap;
    uint transSampler;
    float occlusionStrength;
    uint occlusionMap;
    uint occlusionSampler;
    // The volume behind the surface, for transmission: its index of refraction, how
    // far light crosses it (0 for a thin wall), and how much of it is absorbed — light
    // that has crossed attenuationDistance keeps attenuationColor of itself, none
    // absorbed for a distance of 0.
    float ior;
    float thickness;
    float attenuationDistance;
    vec3 attenuationColor;
    uint emissiveMap;
    uint emissiveSampler;
    // uv2Maps flags the maps that read the mesh's second set of texture coordinates.
    uint uv2Maps;
    // KHR_materials_specular: how much a dielectric reflects, besides its ior.
    // dielectricSpecular is what a dielectric reflects head-on (rgb) and at a grazing
    // angle (a), before the specular maps (see PBRMaterial.dielectricSpecular).
    vec4 dielectricSpecular;
    uint specularIntensityMap;
    uint specularIntensitySampler;
    uint specularColorMap;
    uint specularColorSampler;
    // KHR_materials_clearcoat: a clear coat over the surface.
    float clearcoat;
    float clearcoatRoughness;
    uint clearcoatMap;
    uint clearcoatSampler;
    uint clearcoatRoughnessMap;
    uint clearcoatRoughnessSampler;
    uint clearcoatNormalMap;
    uint clearcoatNormalSampler;
    // KHR_materials_sheen: a sheen over the surface.
    vec3 sheenColor;
    float sheenRoughness;
    uint sheenColorMap;
    uint sheenColorSampler;
    uint sheenRoughnessMap;
    uint sheenRoughnessSampler;
    // KHR_materials_anisotropy: how far, and along which direction, highlights
    // stretch. anisotropyDirection is the rotation's cosine and sine.
    float anisotropy;
    vec2 anisotropyDirection;
    uint anisotropyMap;
    uint anisotropySampler;
    // The transforms of the mesh's two sets of texture coordinates (see materialUVs).
    UVTransform uv1Transform;
    UVTransform uv2Transform;
};
layout(buffer_reference, scalar) readonly buffer MatBuf { Material v[]; };

// tex samples a bindless heap texture at uv with the given sampler.
vec4 tex(uint index, uint samp, vec2 uv) {
    return texture(sampler2D(gTextures[nonuniformEXT(index)], gSamplers[nonuniformEXT(samp)]), uv);
}

// cotangentFrame is the frame of the texture's u and v directions on the surface around
// normal N, with N, derived from the screen-space derivatives of uv and of the
// position — no vertex TANGENT attribute required (Christian Schüler's derivative
// maps). Its u and v are scaled alike, so that it keeps a map's directions as drawn.
mat3 cotangentFrame(vec3 N, vec2 uv) {
    vec3 dp1 = dFdx(vWorldPos);
    vec3 dp2 = dFdy(vWorldPos);
    vec2 duv1 = dFdx(uv);
    vec2 duv2 = dFdy(uv);
    vec3 dp2perp = cross(dp2, N);
    vec3 dp1perp = cross(N, dp1);
    vec3 T = dp2perp * duv1.x + dp1perp * duv2.x;
    vec3 B = dp2perp * duv1.y + dp1perp * duv2.y;
    float invmax = inversesqrt(max(dot(T, T), dot(B, B)));
    return mat3(T * invmax, B * invmax, N);
}

// perturbNormal applies a tangent-space normal (mapN in [-1,1]) to the geometric
// normal N, in the cotangent frame of uv, the coordinates the map was sampled at.
vec3 perturbNormal(vec3 N, vec3 mapN, vec2 uv) {
    return normalize(cotangentFrame(N, uv) * mapN);
}

// readsSameUV reports whether the maps flagged mapFlag and otherFlag read the same set of
// texture coordinates, by uv2Maps (see mapUV).
bool readsSameUV(uint uv2Maps, uint mapFlag, uint otherFlag) {
    return ((uv2Maps & mapFlag) != 0u) == ((uv2Maps & otherFlag) != 0u);
}

// sampleNormalMap returns the tangent-space normal a normal map holds at uv.
//
// Z is reconstructed rather than sampled: a tangent-space normal is a unit vector, so
// the third component carries no independent information. This is what lets normal
// maps be stored two-channel (pix.TextureNormal -> RG8, and BC5 later), and it is
// equally correct for an RGBA8 normal map, whose blue channel holds exactly this value.
vec3 sampleNormalMap(uint index, uint samp, vec2 uv) {
    vec2 nxy = tex(index, samp, uv).rg * 2.0 - 1.0;
    float nz = sqrt(clamp(1.0 - dot(nxy, nxy), 0.0, 1.0));
    return vec3(nxy, nz);
}

// antiAliasedRoughness widens roughness by how much normal varies across the pixel. A
// pixel covering a curved or bumpy patch of a smooth surface sees every normal in it, and
// a highlight narrower than that spread lands in one pixel of a frame and the next pixel
// of the next: smooth, finely curved surfaces sparkle and crawl. Widening the lobe by the
// spread draws the highlight the whole patch would show (Tokuyoshi and Kaplanyan's
// filtering, with their constants: the screen-space variance scale and the most it may
// add). The spread adds to GGX's alpha squared, the lobe's variance, not to roughness:
// added to roughness it widens the lobe by its square, and a mirror-smooth sphere a few
// hundred pixels across keeps a highlight no pixel catches.
float antiAliasedRoughness(float roughness, vec3 normal) {
    vec3 du = dFdx(normal);
    vec3 dv = dFdy(normal);
    float variance = 0.25 * (dot(du, du) + dot(dv, dv));
    float widening = min(2.0 * variance, 0.18);
    float alpha = roughness * roughness;
    return sqrt(sqrt(clamp(alpha * alpha + widening, 0.0, 1.0)));
}

// materialSurface resolves this fragment's Surface from the material record: base
// color (× vertex color × color map), the map-modulated metallic/roughness, and the
// perturbed world normal. baseAlpha is returned separately — it drives forward's
// blending but has nowhere to live in the G-buffer (opaque only).
Surface materialSurface(Material m, MaterialUVs uvs, out float baseAlpha) {
    vec4 base = sampleBase(m.color, m.flags, m.colorMap, m.colorSampler, mapUV(uvs, m.uv2Maps, MAT_COLOR_MAP));
    // An opaque material's alpha is not how much of it is there (see
    // materials.PBRMaterial.SetIgnoresAlpha); glass made of one would vanish by it.
    baseAlpha = (m.flags & MAT_IGNORES_ALPHA) != 0u ? 1.0 : base.a;

    Surface s;
    s.model = MODEL_PBR;
    s.albedo = base.rgb;
    s.emissive = m.emissive.rgb;
    if ((m.flags & MAT_EMISSIVE_MAP) != 0u) {
        s.emissive *= tex(m.emissiveMap, m.emissiveSampler, mapUV(uvs, m.uv2Maps, MAT_EMISSIVE_MAP)).rgb;
    }

    // Metalness, roughness and occlusion are often one texture's blue, green and red
    // (glTF's occlusion-roughness-metallic packing), bound to all three maps: a map
    // with the texture, sampler and set of coordinates of one already read reuses its
    // texel.
    float metallic = m.metallic;
    vec4 metalTexel = vec4(1.0);
    bool hasMetalMap = (m.flags & MAT_METAL_MAP) != 0u;
    if (hasMetalMap) {
        metalTexel = tex(m.metalMap, m.metalSampler, mapUV(uvs, m.uv2Maps, MAT_METAL_MAP));
        metallic *= metalTexel.b;
    }
    float roughness = m.roughness;
    vec4 roughTexel = vec4(1.0);
    bool hasRoughMap = (m.flags & MAT_ROUGH_MAP) != 0u;
    if (hasRoughMap) {
        bool isMetalMap = hasMetalMap && m.roughMap == m.metalMap && m.roughSampler == m.metalSampler &&
                          readsSameUV(m.uv2Maps, MAT_ROUGH_MAP, MAT_METAL_MAP);
        roughTexel = isMetalMap ? metalTexel : tex(m.roughMap, m.roughSampler, mapUV(uvs, m.uv2Maps, MAT_ROUGH_MAP));
        roughness *= roughTexel.g;
    }
    float occlusion = 1.0;
    if ((m.flags & MAT_OCCLUSION_MAP) != 0u) {
        bool isMetalMap = hasMetalMap && m.occlusionMap == m.metalMap && m.occlusionSampler == m.metalSampler &&
                          readsSameUV(m.uv2Maps, MAT_OCCLUSION_MAP, MAT_METAL_MAP);
        bool isRoughMap = hasRoughMap && m.occlusionMap == m.roughMap && m.occlusionSampler == m.roughSampler &&
                          readsSameUV(m.uv2Maps, MAT_OCCLUSION_MAP, MAT_ROUGH_MAP);
        vec4 occlusionTexel = isMetalMap ? metalTexel : (isRoughMap ? roughTexel : tex(m.occlusionMap, m.occlusionSampler, mapUV(uvs, m.uv2Maps, MAT_OCCLUSION_MAP)));
        occlusion = mix(1.0, occlusionTexel.r, m.occlusionStrength);
    }
    s.material = pbrPack(metallic, roughness, occlusion);

    // What a dielectric reflects (KHR_materials_ior and KHR_materials_specular), as the
    // material works it out, times its maps: the intensity map scales both, the colour
    // map tints the head-on reflectance.
    s.specular = m.dielectricSpecular;
    if ((m.flags & MAT_SPECULAR_INTENSITY_MAP) != 0u) {
        s.specular *= tex(m.specularIntensityMap, m.specularIntensitySampler, mapUV(uvs, m.uv2Maps, MAT_SPECULAR_INTENSITY_MAP)).a;
    }
    if ((m.flags & MAT_SPECULAR_COLOR_MAP) != 0u) {
        s.specular.rgb = min(s.specular.rgb * tex(m.specularColorMap, m.specularColorSampler, mapUV(uvs, m.uv2Maps, MAT_SPECULAR_COLOR_MAP)).rgb, vec3(1.0));
    }

    // A back face is drawn only for a double-sided surface, which is lit on the side
    // it is seen from. perturbNormal builds its frame around this normal, so the
    // normal map turns over with it.
    s.normal = gl_FrontFacing ? normalize(vNormal) : -normalize(vNormal);
    if ((m.flags & MAT_NORMAL_MAP) != 0u) {
        s.normal = perturbNormal(s.normal, sampleNormalMap(m.normalMap, m.normalSampler, mapUV(uvs, m.uv2Maps, MAT_NORMAL_MAP)), mapUV(uvs, m.uv2Maps, MAT_NORMAL_MAP));
    }
    s.material.g = antiAliasedRoughness(pbrRoughness(s), s.normal);
    return s;
}

// ---------------------------------------------------------------------------
// Shading a Surface (forward + lighting)
// ---------------------------------------------------------------------------

const float PI = 3.14159265359;

// Shading is what every light's term needs of the surface and the eye, worked out once
// per fragment rather than once per light: which of it depends on the light is only
// what the light's direction changes.
struct Shading {
    vec3 normal;
    vec3 V;
    float NdotV;
    // f0 and f90 are how much of the light meeting it head-on, and at a grazing angle,
    // the surface reflects (pbrF0, pbrF90).
    vec3 f0;
    float f90;
    // alpha is GGX's alpha, the perceptual roughness squared, and alpha2 its square.
    float alpha;
    float alpha2;
    // diffuseAlbedo is the albedo diffuse light scatters, after metal and diffuseScale
    // take their share (see shadeSurface); specularScale scales the reflection.
    vec3 diffuseAlbedo;
    float specularScale;
    // dfg is the split-sum pair (scale, bias) of the surface's roughness seen from V
    // (see environmentBRDF); specularCompensation is what its specular reflection is
    // multiplied by to make up for the light a single GGX bounce loses.
    vec2 dfg;
    vec3 specularCompensation;

    // clearcoat is how much of a clear coat covers the surface, 0 for none (see
    // addClearcoat). The coat has its own normal, NdotV and GGX alpha, and
    // clearcoatBaseScale is what of the light leaving the surface under it gets through:
    // what the coat does not reflect, seen from V.
    float clearcoat;
    vec3 clearcoatNormal;
    float clearcoatNdotV;
    float clearcoatRoughness;
    float clearcoatAlpha;
    float clearcoatBaseScale;

    // sheenColor is the colour of a sheen over the surface, black for none (see
    // addSheen), and sheenRoughness how far it spreads. sheenBaseScale is what of the
    // light leaving the surface under it gets through.
    vec3 sheenColor;
    float sheenRoughness;
    float sheenBaseScale;

    // anisotropy is how far the surface's highlights stretch along anisotropyT, 0 for
    // not at all (see addAnisotropy); anisotropyB is across it. GGX's alpha along each
    // is anisotropyAlphaT and alpha.
    float anisotropy;
    vec3 anisotropyT;
    vec3 anisotropyB;
    float anisotropyAlphaT;
};

// CLEARCOAT_F0 is what the clear coat reflects head-on: a dielectric of index 1.5.
const float CLEARCOAT_F0 = 0.04;

// approximateDFG is the split-sum pair environmentBRDF reads from its table, fitted
// analytically (Karis, "Physically Based Shading on Mobile"): for a scene with no
// environment, which has no table, and a few instructions where the table would be a
// fetch.
vec2 approximateDFG(float NdotV, float roughness) {
    const vec4 c0 = vec4(-1.0, -0.0275, -0.572, 0.022);
    const vec4 c1 = vec4(1.0, 0.0425, 1.04, -0.04);
    vec4 r = roughness * c0 + c1;
    float a004 = min(r.x * r.x, exp2(-9.28 * NdotV)) * r.x + r.y;
    return vec2(-1.04, 1.04) * a004 + r.zw;
}

// specularCompensationOf is what to multiply a GGX reflection by so it reflects all the
// light a rough surface does. GGX counts light reflected once off the microfacets; what
// bounces between them before it leaves is lost, and a rough white metal reflecting a
// white environment comes out a third darker than the white it should be. dfg.x +
// dfg.y is the share a single bounce keeps of what a perfect reflector reflects
// (Fdez-Agüera's multiple-scattering compensation, in Filament's form).
vec3 specularCompensationOf(vec2 dfg, vec3 f0) {
    return 1.0 + f0 * (1.0 / max(dfg.x + dfg.y, 1e-4) - 1.0);
}

// shadingOf works out the Shading of s seen from V. diffuseScale attenuates the
// diffuse light, for glass, whose light passes through instead, and specularScale
// scales the reflection, for a pane's two surfaces reflecting where one is drawn (see
// main).
Shading shadingOf(Surface s, vec3 V, float diffuseScale, float specularScale) {
    Shading sh;
    sh.normal = s.normal;
    sh.V = V;
    sh.NdotV = max(dot(s.normal, V), 0.0);
    sh.f0 = pbrF0(s);
    sh.f90 = pbrF90(s);
    sh.alpha = pbrRoughness(s) * pbrRoughness(s);
    sh.alpha2 = sh.alpha * sh.alpha;
    sh.diffuseAlbedo = s.albedo * (1.0 - pbrMetallic(s)) * diffuseScale;
    sh.specularScale = specularScale;
    if (hasEnvironment(pc.lights)) {
        sh.dfg = environmentBRDF(pc.lights, sh.NdotV, pbrRoughness(s));
    } else {
        sh.dfg = approximateDFG(sh.NdotV, pbrRoughness(s));
    }
    sh.specularCompensation = specularCompensationOf(sh.dfg, sh.f0);
    sh.clearcoat = 0.0;
    sh.clearcoatBaseScale = 1.0;
    sh.sheenColor = vec3(0.0);
    sh.sheenBaseScale = 1.0;
    sh.anisotropy = 0.0;
    return sh;
}

// schlickWeight is Schlick's (1 - cosTheta)^5, as multiplies rather than pow.
float schlickWeight(float cosTheta) {
    float x = clamp(1.0 - cosTheta, 0.0, 1.0);
    float x2 = x * x;
    return x2 * x2 * x;
}

vec3 fresnelSchlick(float cosTheta, vec3 f0, float f90) {
    return f0 + (f90 - f0) * schlickWeight(cosTheta);
}

// addClearcoat covers sh with m's clear coat (KHR_materials_clearcoat). The coat
// lies over the surface's geometric normal, not its normal-mapped one: a lacquer fills
// the bumps under it, unless the coat has a normal map of its own.
void addClearcoat(inout Shading sh, Material m, MaterialUVs uvs) {
    float clearcoat = m.clearcoat;
    if ((m.flags & MAT_CLEARCOAT_MAP) != 0u) {
        clearcoat *= tex(m.clearcoatMap, m.clearcoatSampler, mapUV(uvs, m.uv2Maps, MAT_CLEARCOAT_MAP)).r;
    }
    float roughness = m.clearcoatRoughness;
    if ((m.flags & MAT_CLEARCOAT_ROUGHNESS_MAP) != 0u) {
        roughness *= tex(m.clearcoatRoughnessMap, m.clearcoatRoughnessSampler, mapUV(uvs, m.uv2Maps, MAT_CLEARCOAT_ROUGHNESS_MAP)).g;
    }
    vec3 normal = gl_FrontFacing ? normalize(vNormal) : -normalize(vNormal);
    if ((m.flags & MAT_CLEARCOAT_NORMAL_MAP) != 0u) {
        normal = perturbNormal(normal, sampleNormalMap(m.clearcoatNormalMap, m.clearcoatNormalSampler, mapUV(uvs, m.uv2Maps, MAT_CLEARCOAT_NORMAL_MAP)), mapUV(uvs, m.uv2Maps, MAT_CLEARCOAT_NORMAL_MAP));
    }

    sh.clearcoat = clearcoat;
    sh.clearcoatNormal = normal;
    sh.clearcoatNdotV = max(dot(normal, sh.V), 0.0);
    sh.clearcoatRoughness = antiAliasedRoughness(roughness, normal);
    // Clamped so a mirror coat still spreads a point light into a highlight of some
    // size; at alpha 0 the distribution is infinitely narrow and the light vanishes.
    sh.clearcoatAlpha = max(sh.clearcoatRoughness * sh.clearcoatRoughness, 0.002);
    float reflected = CLEARCOAT_F0 + (1.0 - CLEARCOAT_F0) * schlickWeight(sh.clearcoatNdotV);
    sh.clearcoatBaseScale = 1.0 - clearcoat * reflected;
}

// addAnisotropy stretches sh's highlights along m's direction of anisotropy
// (KHR_materials_anisotropy): its map's direction, or the texture's u, turned by its
// rotation, in the frame of the anisotropy map's coordinates around the surface's
// normal.
void addAnisotropy(inout Shading sh, Material m, MaterialUVs uvs) {
    float strength = m.anisotropy;
    vec2 direction = vec2(1.0, 0.0);
    if ((m.flags & MAT_ANISOTROPY_MAP) != 0u) {
        vec3 texel = tex(m.anisotropyMap, m.anisotropySampler, mapUV(uvs, m.uv2Maps, MAT_ANISOTROPY_MAP)).rgb;
        direction = texel.rg * 2.0 - 1.0;
        strength *= texel.b;
    }
    vec2 rotation = m.anisotropyDirection;
    direction = vec2(rotation.x * direction.x - rotation.y * direction.y, rotation.y * direction.x + rotation.x * direction.y);

    vec3 T = cotangentFrame(sh.normal, mapUV(uvs, m.uv2Maps, MAT_ANISOTROPY_MAP)) * vec3(direction, 0.0);
    T = normalize(T - sh.normal * dot(sh.normal, T));
    sh.anisotropy = strength;
    sh.anisotropyT = T;
    sh.anisotropyB = cross(sh.normal, T);
    sh.anisotropyAlphaT = mix(sh.alpha, 1.0, strength * strength);
}

// anisotropicSpecular returns the GGX specular term — distribution times visibility,
// before Fresnel — of light arriving from L, with the lobe stretched along sh's
// direction of anisotropy (the anisotropic GGX of KHR_materials_anisotropy).
float anisotropicSpecular(Shading sh, vec3 L, vec3 H, float NdotL, float NdotH) {
    float alphaT = sh.anisotropyAlphaT;
    float alphaB = max(sh.alpha, 1e-3);
    float a2 = alphaT * alphaB;
    vec3 f = vec3(alphaB * dot(sh.anisotropyT, H), alphaT * dot(sh.anisotropyB, H), a2 * NdotH);
    float w2 = a2 / max(dot(f, f), 1e-7);
    float distribution = a2 * w2 * w2 / PI;

    float towardV = NdotL * length(vec3(alphaT * dot(sh.anisotropyT, sh.V), alphaB * dot(sh.anisotropyB, sh.V), sh.NdotV));
    float towardL = sh.NdotV * length(vec3(alphaT * dot(sh.anisotropyT, L), alphaB * dot(sh.anisotropyB, L), NdotL));
    float visibility = clamp(0.5 / max(towardV + towardL, 1e-5), 0.0, 1.0);
    return distribution * visibility;
}

// anisotropicReflectionDirection is the direction an anisotropic surface reflects the
// environment from: off a normal bent toward the stretch, so the reflection smears along
// it, as brushed metal's does — the more, the stronger the anisotropy and the rougher
// the surface (three.js's bent normal).
vec3 anisotropicReflectionDirection(Shading sh, float roughness) {
    vec3 bent = normalize(cross(cross(sh.anisotropyB, sh.V), sh.anisotropyB));
    float a = 1.0 - sh.anisotropy * (1.0 - roughness);
    a *= a;
    bent = normalize(mix(bent, sh.normal, a * a));
    return reflect(-sh.V, bent);
}

// addSheen gives sh m's sheen (KHR_materials_sheen).
void addSheen(inout Shading sh, Material m, MaterialUVs uvs) {
    vec3 color = m.sheenColor;
    if ((m.flags & MAT_SHEEN_COLOR_MAP) != 0u) {
        color *= tex(m.sheenColorMap, m.sheenColorSampler, mapUV(uvs, m.uv2Maps, MAT_SHEEN_COLOR_MAP)).rgb;
    }
    float roughness = m.sheenRoughness;
    if ((m.flags & MAT_SHEEN_ROUGHNESS_MAP) != 0u) {
        roughness *= tex(m.sheenRoughnessMap, m.sheenRoughnessSampler, mapUV(uvs, m.uv2Maps, MAT_SHEEN_ROUGHNESS_MAP)).a;
    }
    sh.sheenColor = color;
    // Below about 0.07 the distribution's exponent grows past what float holds.
    sh.sheenRoughness = clamp(roughness, 0.07, 1.0);
    // What the sheen reflects, averaged over the angles light meets it at: 0.157 of its
    // brightest channel (three.js's fit of the sheen's directional albedo).
    sh.sheenBaseScale = 1.0 - 0.157 * max(color.r, max(color.g, color.b));
}

// sheenSpecular returns the radiance one light, arriving from L with radiance, reflects
// off the sheen toward the eye: Estevez and Kulla's "Charlie" distribution of fibres,
// with Neubelt and Pettineo's visibility.
vec3 sheenSpecular(Shading sh, vec3 L, vec3 radiance) {
    vec3 H = normalize(sh.V + L);
    float NdotL = max(dot(sh.normal, L), 0.0);
    float NdotH = max(dot(sh.normal, H), 0.0);

    float alpha = sh.sheenRoughness * sh.sheenRoughness;
    float invAlpha = 1.0 / alpha;
    float sin2h = max(1.0 - NdotH * NdotH, 0.0078125);
    float distribution = (2.0 + invAlpha) * pow(sin2h, invAlpha * 0.5) / (2.0 * PI);
    float visibility = clamp(1.0 / (4.0 * (NdotL + sh.NdotV - NdotL * sh.NdotV)), 0.0, 1.0);
    return sh.sheenColor * (distribution * visibility * NdotL) * radiance;
}

// sheenReflectance is the share of the light around it a white sheen reflects toward V:
// the sheen's directional albedo, fitted (three.js's IBLSheenBRDF).
float sheenReflectance(float NdotV, float roughness) {
    float r2 = roughness * roughness;
    float a = roughness < 0.25 ? -339.2 * r2 + 161.4 * roughness - 25.9 : -8.48 * r2 + 14.3 * roughness - 9.95;
    float b = roughness < 0.25 ? 44.0 * r2 - 23.7 * roughness + 3.26 : 1.97 * r2 - 3.27 * roughness + 0.72;
    float dg = exp(a * NdotV + b) + (roughness < 0.25 ? 0.0 : 0.1 * (roughness - 0.25));
    return clamp(dg / PI, 0.0, 1.0);
}

// clearcoatSpecular returns the radiance one light, arriving from L with radiance,
// reflects off the clear coat toward the eye: the same GGX lobe as cookTorrance's, over
// the coat's normal and roughness, with no diffuse.
vec3 clearcoatSpecular(Shading sh, vec3 L, vec3 radiance) {
    vec3 H = normalize(sh.V + L);
    float NdotL = max(dot(sh.clearcoatNormal, L), 0.0);
    float NdotH = max(dot(sh.clearcoatNormal, H), 0.0);
    float VdotH = max(dot(sh.V, H), 0.0);

    float alpha2 = sh.clearcoatAlpha * sh.clearcoatAlpha;
    float d = NdotH * NdotH * (alpha2 - 1.0) + 1.0;
    float distribution = alpha2 / max(PI * d * d, 1e-5);
    float visibility = smithVisibility(sh.clearcoatNdotV, NdotL, sh.clearcoatAlpha);
    float fresnel = CLEARCOAT_F0 + (1.0 - CLEARCOAT_F0) * schlickWeight(VdotH);
    return vec3(distribution * visibility * fresnel * NdotL) * radiance;
}

// cookTorrance returns the radiance one light, arriving from L with radiance, leaves the
// surface with toward the eye: GGX distribution, height-correlated Smith visibility,
// Schlick Fresnel.
vec3 cookTorrance(Shading sh, vec3 L, vec3 radiance) {
    vec3 H = normalize(sh.V + L);
    float NdotL = max(dot(sh.normal, L), 0.0);
    float NdotH = max(dot(sh.normal, H), 0.0);
    float VdotH = max(dot(sh.V, H), 0.0);

    float lobe;
    if (ANISOTROPY && sh.anisotropy > 0.0) {
        lobe = anisotropicSpecular(sh, L, H, NdotL, NdotH);
    } else {
        float d = NdotH * NdotH * (sh.alpha2 - 1.0) + 1.0;
        float distribution = sh.alpha2 / max(PI * d * d, 1e-5);
        lobe = distribution * smithVisibility(sh.NdotV, NdotL, sh.alpha);
    }
    vec3 fresnel = fresnelSchlick(VdotH, sh.f0, sh.f90);

    vec3 specular = lobe * fresnel * sh.specularCompensation * sh.specularScale;
    vec3 diffuse = (vec3(1.0) - fresnel) * sh.diffuseAlbedo * (1.0 / PI);
    vec3 lit = (diffuse + specular) * radiance * NdotL;
    if (SHEEN) {
        lit = lit * sh.sheenBaseScale + sheenSpecular(sh, L, radiance);
    }
    if (CLEARCOAT && sh.clearcoat > 0.0) {
        lit = lit * sh.clearcoatBaseScale + sh.clearcoat * clearcoatSpecular(sh, L, radiance);
    }
    return lit;
}

// environmentLight is what a surface takes from the scene's environment, in place of
// the flat ambient colour: its diffuse light, less the share Fresnel reflects instead,
// and its reflection of the environment, blurred for its roughness and weighted by the
// split-sum table. The Shading's diffuse albedo and specular scale scale each, as in
// cookTorrance.
vec3 environmentLight(LightBuf L, Surface s, Shading sh) {
    float rough = pbrRoughness(s);
    vec3 reflected = sh.f0 * sh.dfg.x + sh.f90 * sh.dfg.y;
    vec3 direction = reflect(-sh.V, sh.normal);
    if (ANISOTROPY && sh.anisotropy > 0.0) {
        direction = anisotropicReflectionDirection(sh, rough);
    }
    vec3 specular = environmentSpecular(L, direction, rough) * reflected * sh.specularCompensation * sh.specularScale;
    vec3 diffuse = (vec3(1.0) - reflected) * sh.diffuseAlbedo * environmentDiffuse(L, sh.normal);
    return diffuse + specular;
}

// shadeSurface accumulates every light in the table onto a Surface and returns the
// LINEAR result (ambient + direct + emissive) — the caller encodes it once — and in
// indirect its ambient (or environment) share. diffuseScale scales its diffuse light,
// as in shadingOf. receives lets the forward path honour a drawable's receive-shadow
// flag.
// (The light table is read from the push constants rather than passed in: a buffer_reference
// can't cross a function parameter without dropping its readonly qualifier, and both
// passes that compile this function expose it as pc.lights.)
vec3 shadeSurface(Surface s, Shading sh, vec3 worldPos, float diffuseScale, uint shadowSamp, bool receives, out vec3 indirect) {
    LightBuf L = pc.lights;
    // Indirect light first: it is all that needs the Surface beyond its Shading, which is
    // then free before the light loops, which hold a lot else.
    vec3 ambient = L.ambient.rgb * s.albedo * diffuseScale;
    if (hasEnvironment(L)) {
        ambient = environmentLight(L, s, sh);
    }
    // Baked occlusion takes its share of the light from around the surface; ambient
    // occlusion, measured on screen, then takes its share of what is left (see
    // outputAlpha in material_common.glsl).
    ambient *= pbrOcclusion(s);
    vec3 emissive = s.emissive;
    if (SHEEN) {
        // The sheen reflects the light around it, the environment's or the flat ambient
        // colour, as diffuse light does: from all of the hemisphere.
        vec3 around = hasEnvironment(L) ? environmentDiffuse(L, sh.normal) : L.ambient.rgb;
        vec3 sheen = around * sh.sheenColor * sheenReflectance(sh.NdotV, sh.sheenRoughness);
        ambient = ambient * sh.sheenBaseScale + sheen * pbrOcclusion(s);
        emissive *= sh.sheenBaseScale;
    }
    if (CLEARCOAT && sh.clearcoat > 0.0) {
        // What the coat reflects of the environment, and what of the light from the
        // surface under it — its own glow included — the coat lets through. Under a flat
        // ambient colour the coat reflects nothing, as the surface under it does not.
        ambient *= sh.clearcoatBaseScale;
        emissive *= sh.clearcoatBaseScale;
        if (hasEnvironment(L)) {
            vec2 brdf = environmentBRDF(L, sh.clearcoatNdotV, sh.clearcoatRoughness);
            vec3 reflection = environmentSpecular(L, reflect(-sh.V, sh.clearcoatNormal), sh.clearcoatRoughness);
            ambient += sh.clearcoat * reflection * (CLEARCOAT_F0 * brdf.x + brdf.y) * pbrOcclusion(s);
        }
    }
    indirect = ambient;
    vec3 lo = ambient + emissive;

    for (uint i = 0u; i < L.numDir; i++) {
        vec3 Ldir = normalize(-L.dirs[i].dir.xyz);
        // A surface turned away from a light receives nothing from it: cookTorrance
        // multiplies by max(dot(N,L),0) and returns exactly zero here, so skipping is
        // not an approximation. It is worth doing because the shadow lookup is not free
        // — up to nine texture fetches for a result about to be multiplied by zero — and
        // roughly half the fragments in a closed scene face away from any given light.
        if (dot(sh.normal, Ldir) <= 0.0) continue;
        float shadow = receives ? dirShadowFactor(L, i, worldPos, sh.normal, shadowSamp) : 1.0;
        vec3 radiance = L.dirs[i].color.rgb * L.dirs[i].color.w * dirMask(L, i, worldPos);
        lo += shadow * cookTorrance(sh, Ldir, radiance);
    }
    // Point and spot lights: only those the fragment's cluster lists.
    uint cluster = lightCluster(L, worldPos);
    uint pointCount = clusterPointCount(L, cluster);
    uint spotCount = clusterSpotCount(L, cluster);
    for (uint i = 0u; i < pointCount; i++) {
        uint li = clusterLight(L, cluster, i);
        PointLight pl = L.points.v[li];
        vec3 d = pl.pos.xyz - worldPos;
        float dist = length(d);
        float atten = pointAttenuation(pl, dist);
        vec3 Ldir = d / max(dist, 0.0001);
        if (atten <= 0.0 || dot(sh.normal, Ldir) <= 0.0) continue;
        float shadow = receives ? pointShadowFactor(L, li, worldPos, shadowSamp) : 1.0;
        lo += shadow * cookTorrance(sh, Ldir, pl.color.rgb * pl.color.w * atten);
    }
    for (uint i = 0u; i < spotCount; i++) {
        uint li = clusterLight(L, cluster, pointCount + i);
        SpotLight sl = L.spots.v[li];
        vec3 d = sl.pos.xyz - worldPos;
        float dist = length(d);
        vec3 Ldir = d / max(dist, 1e-4);
        float atten = spotAttenuation(sl, worldPos, Ldir, dist);
        if (atten <= 0.0 || dot(sh.normal, Ldir) <= 0.0) continue;
        float shadow = receives ? spotShadowFactor(L, li, worldPos, shadowSamp) : 1.0;
        lo += shadow * cookTorrance(sh, Ldir, sl.color.rgb * sl.color.w * atten);
    }
    return lo;
}

// fresnelReflectance is the share of the light meeting the surface from V that it
// reflects rather than lets through: Schlick's approximation, as one number.
//
// abs() because glass is double-sided: a back face has the normal pointing away, and
// a signed dot would read every one of them as pure grazing.
float fresnelReflectance(Surface s, vec3 V) {
    float f0 = dot(pbrF0(s), vec3(0.2126, 0.7152, 0.0722));
    float ndv = abs(dot(s.normal, V));
    return f0 + (pbrF90(s) - f0) * schlickWeight(ndv);
}

// sceneBehind is the light from behind the surface that comes through it toward the
// eye, read from the scene copy (see pix.sceneCopy): where the view ray, bent as it
// enters by the surface's index of refraction, leaves the volume behind it — a thin wall
// does not move it — and at a blurrier level the rougher the surface, as rough glass
// spreads what passes through it. The volume absorbs some of it over the distance
// crossed, and the surface tints what is left by its colour (KHR_materials_transmission
// and KHR_materials_volume).
//
// TODO: check depth where the bent ray lands. With a thickness it can land on something
// in front of the glass, which then smears into it; read the scene's depth there, and
// fall back toward the unbent position where what is there is nearer than the glass.
// TODO: read the environment along the refracted ray where it leaves the screen, rather
// than the screen's edge through the clamping sampler.
// TODO: scale thickness by the mesh's transform: glTF measures it in the mesh's own
// units, this in world units. KHR_materials_volume's thickness texture is not read.
// TODO: glass behind glass is not refracted: the copy holds the opaque scene only.
vec3 sceneBehind(Material m, Surface s, vec3 V) {
    vec3 refracted = refract(-V, s.normal, 1.0 / max(m.ior, 1.0));
    vec3 exit = vWorldPos + refracted * m.thickness;
    vec4 clip = pc.viewProj * vec4(exit, 1.0);
    vec2 uv = clip.xy / clip.w * 0.5 + 0.5;

    // The blur widens with roughness, and with how strongly the surface bends light:
    // through an index of 1 nothing bends, and nothing blurs.
    float levels = float(textureQueryLevels(sampler2D(gTextures[nonuniformEXT(pc.sceneCopy)], gSamplers[nonuniformEXT(pc.sceneCopySampler)])));
    float lod = (levels - 1.0) * pbrRoughness(s) * clamp(m.ior * 2.0 - 2.0, 0.0, 1.0);
    vec3 light = textureLod(sampler2D(gTextures[nonuniformEXT(pc.sceneCopy)], gSamplers[nonuniformEXT(pc.sceneCopySampler)]), uv, lod).rgb;

    if (m.attenuationDistance > 0.0 && m.thickness > 0.0) {
        light *= pow(m.attenuationColor, vec3(m.thickness / m.attenuationDistance));
    }
    return light * s.albedo;
}

// ---------------------------------------------------------------------------
void main() {
    discardCutOut();
    Material m = MatBuf(pc.materials).v[vMat];
    float baseAlpha;
    MaterialUVs uvs = materialUVs(m.uv1Transform, m.uv2Transform);
    Surface s = materialSurface(m, uvs, baseAlpha);
    vec3 toEye = pc.eye.xyz - vWorldPos;
    float viewDist = length(toEye);
    vec3 V = toEye / viewDist;
    bool receives = (vFlags & FLAG_RECEIVES_SHADOW) != 0u;

    if (!TRANSMISSION) {
        vec3 indirect;
        Shading sh = shadingOf(s, V, 1.0, 1.0);
        if (ANISOTROPY) {
            addAnisotropy(sh, m, uvs);
        }
        if (SHEEN) {
            addSheen(sh, m, uvs);
        }
        if (CLEARCOAT) {
            addClearcoat(sh, m, uvs);
        }
        vec3 unfogged = shadeSurface(s, sh, vWorldPos, 1.0, pc.shadowSampler, receives, indirect);
        Fog fog = fogAt(viewDist, pc.lights.fogColor, pc.lights.fogParams);
        vec3 lit = applyFog(unfogged, fog);
        outColor = vec4(lit, outputAlpha(lit, indirect, fog, baseAlpha)); // linear: the target encodes it for display
        return;
    }

    // Transmission (glass): the share of the light that passes through the surface takes
    // the place of its diffuse term, and the scene behind it shows through instead
    // (KHR_materials_transmission).
    //
    // The map (red channel, per the extension) is what keeps a partly-glass object
    // from going uniformly transparent: without it a cabinet with glass panes turns
    // the whole cabinet into a ghost.
    float transmission = m.transmission;
    if ((m.flags & MAT_TRANS_MAP) != 0u) transmission *= tex(m.transMap, m.transSampler, mapUV(uvs, m.uv2Maps, MAT_TRANS_MAP)).r;

    // reflectance is what one surface of the glass reflects of the light meeting it, and
    // passedShare what comes through of the light from behind: what the surface
    // transmits of what it does not reflect.
    float reflectance = fresnelReflectance(s, V);
    float passedShare = transmission * (1.0 - reflectance);
    float specularScale = 1.0;
    if (m.thickness <= 0.0) {
        // With no thickness the surface stands for a whole pane, front and back. Each
        // reflects R, and light bounces between them, so of the light the front lets
        // in, (1 - R) / (1 + R) leaves by the back, and the back sends that share of
        // the front's reflection out again: a pane reflects 2R / (1 + R), not R.
        // Without the second surface a window reflects half of what it should, and in
        // a bright scene all but vanishes.
        // TODO: a box pane with two glass faces and no thickness, drawn without the
        // scene copy, blends both faces and reflects about twice what it should; with
        // the copy the front face covers the back.
        float throughBack = (1.0 - reflectance) / (1.0 + reflectance);
        passedShare = transmission * throughBack;
        specularScale = 1.0 + transmission * throughBack;
    }

    vec3 indirect;
    Shading sh = shadingOf(s, V, 1.0 - transmission, specularScale);
    if (ANISOTROPY) {
        addAnisotropy(sh, m, uvs);
    }
    if (SHEEN) {
        addSheen(sh, m, uvs);
    }
    if (CLEARCOAT) {
        addClearcoat(sh, m, uvs);
    }
    vec3 unfogged = shadeSurface(s, sh, vWorldPos, 1.0 - transmission, pc.shadowSampler, receives, indirect);
    Fog fog = fogAt(viewDist, pc.lights.fogColor, pc.lights.fogParams);
    if (transmission <= 0.0) {
        vec3 lit = applyFog(unfogged, fog);
        outColor = vec4(lit, outputAlpha(lit, indirect, fog, baseAlpha)); // linear: the target encodes it for display
        return;
    }

    // Glass is drawn premultiplied (materials.BlendPremultiplied, which SetTransmission
    // selects): its colour is the light it reflects, added in full, never scaled by how
    // much of the scene behind it comes through.
    // shadeSurface reflects the environment where the scene has one. Without one the
    // surroundings are the ambient colour; leaving them out would turn the grazing rim,
    // which passes almost nothing through, black.
    if (!hasEnvironment(pc.lights)) {
        vec3 rim = reflectance * specularScale * transmission * pc.lights.ambient.rgb;
        unfogged += rim;
        indirect += rim;
    }

    // Fog scales what it covers and adds its own light. The scene behind already carries
    // the fog in front of the glass, so the glass adds the fog's light only for the share
    // of the scene it keeps out.
    vec3 lit = applyFog(unfogged, fog);

    if (pc.sceneCopy == NO_SCENE_COPY) {
        // No copy of the scene to read (see pix.sceneCopy): the scene behind shows
        // through by blending, neither bent nor tinted, and alpha is the share of it
        // the glass keeps out.
        float alpha = baseAlpha * (1.0 - passedShare);
        lit -= fog.light * (1.0 - alpha);
        outColor = vec4(lit, outputAlpha(lit, indirect, fog, alpha));
        return;
    }

    // With the scene copy the glass draws what is behind it itself, and covers the
    // scene in full where its colour's alpha does.
    lit -= fog.light * passedShare;
    lit += passedShare * sceneBehind(m, s, V);
    outColor = vec4(lit * baseAlpha, baseAlpha);
}
