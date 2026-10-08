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

// This model's id and its interpretation of Surface.material (the model-defined
// G-buffer slot). Every pass goes through these two helpers, so the packing is stated
// once: another shading model packs its own parameters into the same channels.
const uint MODEL_PBR = 0u;

vec4 pbrPack(float metallic, float roughness, float occlusion, float dielectricF0) {
    return vec4(metallic, roughness, occlusion, dielectricF0);
}

float pbrMetallic(Surface s) { return s.material.r; }
float pbrRoughness(Surface s) { return s.material.g; }
// pbrOcclusion is how much of the light around it reaches the surface, by its occlusion
// map: what ambient and environment light are scaled by.
float pbrOcclusion(Surface s) { return s.material.b; }
// pbrDielectricF0 is how much of the light meeting it head-on the surface reflects
// where it is not metal, by its index of refraction: 0.04 for the usual 1.5.
float pbrDielectricF0(Surface s) { return s.material.a; }

// pbrF0 is how much of the light meeting it head-on the surface reflects, in colour:
// its dielectric reflectance, or where it is metal, its albedo.
vec3 pbrF0(Surface s) {
    return mix(vec3(pbrDielectricF0(s)), s.albedo, pbrMetallic(s));
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

// Material mirrors materials.PBRMaterial's record (132 bytes). Each map carries its own
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
};
layout(buffer_reference, scalar) readonly buffer MatBuf { Material v[]; };

// tex samples a bindless heap texture at this fragment's UV with the given sampler.
vec4 tex(uint index, uint samp) {
    return texture(sampler2D(gTextures[nonuniformEXT(index)], gSamplers[nonuniformEXT(samp)]), vUV);
}

// perturbNormal applies a tangent-space normal (mapN in [-1,1]) to the geometric
// normal N using a cotangent frame derived from screen-space derivatives — no vertex
// TANGENT attribute required (Christian Schüler's derivative maps).
vec3 perturbNormal(vec3 N, vec3 mapN) {
    vec3 dp1 = dFdx(vWorldPos);
    vec3 dp2 = dFdy(vWorldPos);
    vec2 duv1 = dFdx(vUV);
    vec2 duv2 = dFdy(vUV);
    vec3 dp2perp = cross(dp2, N);
    vec3 dp1perp = cross(N, dp1);
    vec3 T = dp2perp * duv1.x + dp1perp * duv2.x;
    vec3 B = dp2perp * duv1.y + dp1perp * duv2.y;
    float invmax = inversesqrt(max(dot(T, T), dot(B, B)));
    mat3 TBN = mat3(T * invmax, B * invmax, N);
    return normalize(TBN * mapN);
}

// materialSurface resolves this fragment's Surface from the material record: base
// color (× vertex color × color map), the map-modulated metallic/roughness, and the
// perturbed world normal. baseAlpha is returned separately — it drives forward's
// blending but has nowhere to live in the G-buffer (opaque only).
Surface materialSurface(Material m, out float baseAlpha) {
    vec4 base = sampleBase(m.color, m.flags, m.colorMap, m.colorSampler);
    // An opaque material's alpha is not how much of it is there (see
    // materials.PBRMaterial.SetIgnoresAlpha); glass made of one would vanish by it.
    baseAlpha = (m.flags & MAT_IGNORES_ALPHA) != 0u ? 1.0 : base.a;

    Surface s;
    s.model = MODEL_PBR;
    s.albedo = base.rgb;
    s.emissive = m.emissive.rgb;
    if ((m.flags & MAT_EMISSIVE_MAP) != 0u) {
        s.emissive *= tex(m.emissiveMap, m.emissiveSampler).rgb;
    }

    // Metalness, roughness and occlusion are often one texture's blue, green and red
    // (glTF's occlusion-roughness-metallic packing), bound to all three maps: a map
    // with the texture and sampler of one already read reuses its texel.
    float metallic = m.metallic;
    vec4 metalTexel = vec4(1.0);
    bool hasMetalMap = (m.flags & MAT_METAL_MAP) != 0u;
    if (hasMetalMap) {
        metalTexel = tex(m.metalMap, m.metalSampler);
        metallic *= metalTexel.b;
    }
    float roughness = m.roughness;
    vec4 roughTexel = vec4(1.0);
    bool hasRoughMap = (m.flags & MAT_ROUGH_MAP) != 0u;
    if (hasRoughMap) {
        bool isMetalMap = hasMetalMap && m.roughMap == m.metalMap && m.roughSampler == m.metalSampler;
        roughTexel = isMetalMap ? metalTexel : tex(m.roughMap, m.roughSampler);
        roughness *= roughTexel.g;
    }
    float occlusion = 1.0;
    if ((m.flags & MAT_OCCLUSION_MAP) != 0u) {
        bool isMetalMap = hasMetalMap && m.occlusionMap == m.metalMap && m.occlusionSampler == m.metalSampler;
        bool isRoughMap = hasRoughMap && m.occlusionMap == m.roughMap && m.occlusionSampler == m.roughSampler;
        vec4 occlusionTexel = isMetalMap ? metalTexel : (isRoughMap ? roughTexel : tex(m.occlusionMap, m.occlusionSampler));
        occlusion = mix(1.0, occlusionTexel.r, m.occlusionStrength);
    }
    // KHR_materials_ior: reflectance head-on is ((ior - 1) / (ior + 1))^2.
    float ratio = (m.ior - 1.0) / (m.ior + 1.0);
    s.material = pbrPack(metallic, roughness, occlusion, ratio * ratio);

    // A back face is drawn only for a double-sided surface, which is lit on the side
    // it is seen from. perturbNormal builds its frame around this normal, so the
    // normal map turns over with it.
    s.normal = gl_FrontFacing ? normalize(vNormal) : -normalize(vNormal);
    if ((m.flags & MAT_NORMAL_MAP) != 0u) {
        // Z is reconstructed rather than sampled: a tangent-space normal is a unit
        // vector, so the third component carries no independent information. This
        // is what lets normal maps be stored two-channel (pix.TextureNormal -> RG8,
        // and BC5 later), and it is equally correct for an RGBA8 normal map, whose
        // blue channel holds exactly this value.
        vec2 nxy = tex(m.normalMap, m.normalSampler).rg * 2.0 - 1.0;
        float nz = sqrt(clamp(1.0 - dot(nxy, nxy), 0.0, 1.0));
        s.normal = perturbNormal(s.normal, vec3(nxy, nz));
    }
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
    // f0 is how much of the light meeting it head-on the surface reflects (pbrF0).
    vec3 f0;
    // alpha is GGX's alpha, the perceptual roughness squared, and alpha2 its square.
    float alpha;
    float alpha2;
    // diffuseAlbedo is the albedo diffuse light scatters, after metal and diffuseScale
    // take their share (see shadeSurface); specularScale scales the reflection.
    vec3 diffuseAlbedo;
    float specularScale;
};

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
    sh.alpha = pbrRoughness(s) * pbrRoughness(s);
    sh.alpha2 = sh.alpha * sh.alpha;
    sh.diffuseAlbedo = s.albedo * (1.0 - pbrMetallic(s)) * diffuseScale;
    sh.specularScale = specularScale;
    return sh;
}

// schlickWeight is Schlick's (1 - cosTheta)^5, as multiplies rather than pow.
float schlickWeight(float cosTheta) {
    float x = clamp(1.0 - cosTheta, 0.0, 1.0);
    float x2 = x * x;
    return x2 * x2 * x;
}

vec3 fresnelSchlick(float cosTheta, vec3 f0) {
    return f0 + (1.0 - f0) * schlickWeight(cosTheta);
}

// cookTorrance returns the radiance one light, arriving from L with radiance, leaves the
// surface with toward the eye: GGX distribution, height-correlated Smith visibility,
// Schlick Fresnel.
vec3 cookTorrance(Shading sh, vec3 L, vec3 radiance) {
    vec3 H = normalize(sh.V + L);
    float NdotL = max(dot(sh.normal, L), 0.0);
    float NdotH = max(dot(sh.normal, H), 0.0);
    float VdotH = max(dot(sh.V, H), 0.0);

    float d = NdotH * NdotH * (sh.alpha2 - 1.0) + 1.0;
    float distribution = sh.alpha2 / max(PI * d * d, 1e-5);
    float visibility = smithVisibility(sh.NdotV, NdotL, sh.alpha);
    vec3 fresnel = fresnelSchlick(VdotH, sh.f0);

    vec3 specular = distribution * visibility * fresnel * sh.specularScale;
    vec3 diffuse = (vec3(1.0) - fresnel) * sh.diffuseAlbedo * (1.0 / PI);
    return (diffuse + specular) * radiance * NdotL;
}

// environmentLight is what a surface takes from the scene's environment, in place of
// the flat ambient colour: its diffuse light, less the share Fresnel reflects instead,
// and its reflection of the environment, blurred for its roughness and weighted by the
// split-sum table. The Shading's diffuse albedo and specular scale scale each, as in
// cookTorrance.
vec3 environmentLight(LightBuf L, Surface s, Shading sh) {
    float rough = pbrRoughness(s);
    vec2 brdf = environmentBRDF(L, sh.NdotV, rough);
    vec3 reflected = sh.f0 * brdf.x + brdf.y;
    vec3 specular = environmentSpecular(L, reflect(-sh.V, sh.normal), rough) * reflected * sh.specularScale;
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
    indirect = ambient;
    vec3 lo = ambient + s.emissive;

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
    return f0 + (1.0 - f0) * schlickWeight(ndv);
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
    Surface s = materialSurface(m, baseAlpha);
    vec3 toEye = pc.eye.xyz - vWorldPos;
    float viewDist = length(toEye);
    vec3 V = toEye / viewDist;
    bool receives = (vFlags & FLAG_RECEIVES_SHADOW) != 0u;

    if (!TRANSMISSION) {
        vec3 indirect;
        Shading sh = shadingOf(s, V, 1.0, 1.0);
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
    if ((m.flags & MAT_TRANS_MAP) != 0u) transmission *= tex(m.transMap, m.transSampler).r;

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
