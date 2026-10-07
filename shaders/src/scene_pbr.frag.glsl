// scene_pbr.frag.glsl — PBRMaterial's fragment shader: surface + lighting in one pass.
//
// It resolves a Surface from the material record (materialSurface), then shades that
// Surface against the light table (shadeSurface). The split is not structural any more
// — both halves run here, back to back — but it keeps the per-material-type half
// separate from the half every shading model shares.
//
// TODO: review its cost (see pix.materialPipeline for specialization constants). Its
// record grew from 100 bytes to 132, reflectance head-on is derived from the index of
// refraction per fragment, and every light pays the pane's reflection scale. Compare how
// three.js (its meshphysical chunks, compiled per material with defines) and Godot (its
// generated scene shaders and specialization-constant variants) split the same work,
// then profile this one branch by branch.
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
    baseAlpha = base.a;

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

float distributionGGX(vec3 N, vec3 H, float rough) {
    float a = rough * rough;
    float a2 = a * a;
    float ndh = max(dot(N, H), 0.0);
    float d = ndh * ndh * (a2 - 1.0) + 1.0;
    return a2 / max(PI * d * d, 1e-5);
}

float geometrySchlickGGX(float ndv, float rough) {
    float r = rough + 1.0;
    float k = (r * r) / 8.0;
    return ndv / (ndv * (1.0 - k) + k);
}

float geometrySmith(vec3 N, vec3 V, vec3 L, float rough) {
    return geometrySchlickGGX(max(dot(N, V), 0.0), rough) * geometrySchlickGGX(max(dot(N, L), 0.0), rough);
}

vec3 fresnelSchlick(float cosT, vec3 f0) {
    return f0 + (1.0 - f0) * pow(clamp(1.0 - cosT, 0.0, 1.0), 5.0);
}

// cookTorrance returns the radiance one light leaves s with toward V. diffuseScale
// attenuates the diffuse term for glass, whose light passes through instead, and
// specularScale scales the reflection, for a pane's two surfaces reflecting where one
// is drawn (see main).
vec3 cookTorrance(Surface s, vec3 V, vec3 L, vec3 radiance, float diffuseScale, float specularScale) {
    vec3 N = s.normal;
    vec3 albedo = s.albedo;
    float metallic = pbrMetallic(s);
    float rough = pbrRoughness(s);
    vec3 H = normalize(V + L);
    vec3 f0 = pbrF0(s);
    float ndf = distributionGGX(N, H, rough);
    float g = geometrySmith(N, V, L, rough);
    vec3 f = fresnelSchlick(max(dot(H, V), 0.0), f0);
    vec3 spec = (ndf * g * f) / max(4.0 * max(dot(N, V), 0.0) * max(dot(N, L), 0.0), 1e-4);
    vec3 kd = (vec3(1.0) - f) * (1.0 - metallic);
    float ndl = max(dot(N, L), 0.0);
    return (kd * albedo / PI * diffuseScale + spec * specularScale) * radiance * ndl;
}

// environmentLight is what a surface takes from the scene's environment, in place of
// the flat ambient colour: its diffuse light, less the share Fresnel reflects instead,
// and its reflection of the environment, blurred for its roughness and weighted by the
// split-sum table. diffuseScale and specularScale scale each, as in cookTorrance.
vec3 environmentLight(LightBuf L, Surface s, vec3 V, float diffuseScale, float specularScale) {
    float rough = pbrRoughness(s);
    float metal = pbrMetallic(s);
    vec3 f0 = pbrF0(s);
    vec2 brdf = environmentBRDF(L, max(dot(s.normal, V), 0.0), rough);
    vec3 reflected = f0 * brdf.x + brdf.y;
    vec3 specular = environmentSpecular(L, reflect(-V, s.normal), rough) * reflected * specularScale;
    vec3 diffuse = (vec3(1.0) - reflected) * (1.0 - metal) * s.albedo * environmentDiffuse(L, s.normal) * diffuseScale;
    return diffuse + specular;
}

// shadeSurface accumulates every light in the table onto a Surface and returns the
// LINEAR result (ambient + direct + emissive) — the caller encodes it once — and in
// indirect its ambient (or environment) share. diffuseScale and specularScale scale its
// diffuse light and its reflection, as in cookTorrance. receives lets the forward path
// honour a drawable's receive-shadow flag.
// (The light table is read from the push constants rather than passed in: a buffer_reference
// can't cross a function parameter without dropping its readonly qualifier, and both
// passes that compile this function expose it as pc.lights.)
vec3 shadeSurface(Surface s, vec3 worldPos, vec3 V, uint shadowSamp, float diffuseScale, float specularScale, bool receives, out vec3 indirect) {
    LightBuf L = pc.lights;
    vec3 lo = vec3(0.0);
    // Every directional light selects its cascade on this, and it does not vary between
    // them.
    float viewDist = length(pc.eye.xyz - worldPos);
    for (uint i = 0u; i < L.numDir; i++) {
        vec3 Ldir = normalize(-L.dirs[i].dir.xyz);
        // A surface turned away from a light receives nothing from it: cookTorrance
        // multiplies by max(dot(N,L),0) and returns exactly zero here, so skipping is
        // not an approximation. It is worth doing because the shadow lookup is not free
        // — up to nine texture fetches for a result about to be multiplied by zero — and
        // roughly half the fragments in a closed scene face away from any given light.
        if (dot(s.normal, Ldir) <= 0.0) continue;
        float sh = receives ? dirShadowFactor(L, i, worldPos, s.normal, viewDist, shadowSamp) : 1.0;
        vec3 radiance = L.dirs[i].color.rgb * L.dirs[i].color.w * dirMask(L, i, worldPos);
        lo += sh * cookTorrance(s, V, Ldir, radiance, diffuseScale, specularScale);
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
        if (atten <= 0.0 || dot(s.normal, Ldir) <= 0.0) continue;
        float sh = receives ? pointShadowFactor(L, li, worldPos, shadowSamp) : 1.0;
        lo += sh * cookTorrance(s, V, Ldir, pl.color.rgb * pl.color.w * atten, diffuseScale, specularScale);
    }
    for (uint i = 0u; i < spotCount; i++) {
        uint li = clusterLight(L, cluster, pointCount + i);
        SpotLight sl = L.spots.v[li];
        vec3 d = sl.pos.xyz - worldPos;
        float dist = length(d);
        vec3 Ldir = d / max(dist, 1e-4);
        float atten = spotAttenuation(sl, worldPos, Ldir, dist);
        if (atten <= 0.0 || dot(s.normal, Ldir) <= 0.0) continue;
        float sh = receives ? spotShadowFactor(L, li, worldPos, shadowSamp) : 1.0;
        lo += sh * cookTorrance(s, V, Ldir, sl.color.rgb * sl.color.w * atten, diffuseScale, specularScale);
    }
    vec3 ambient = L.ambient.rgb * s.albedo * diffuseScale;
    if (hasEnvironment(L)) {
        ambient = environmentLight(L, s, V, diffuseScale, specularScale);
    }
    // Baked occlusion takes its share of the light from around the surface; ambient
    // occlusion, measured on screen, then takes its share of what is left (see
    // outputAlpha in material_common.glsl).
    ambient *= pbrOcclusion(s);
    indirect = ambient;
    return ambient + lo + s.emissive;
}

// fresnelReflectance is the share of the light meeting the surface from V that it
// reflects rather than lets through: Schlick's approximation, as one number.
//
// abs() because glass is double-sided: a back face has the normal pointing away, and
// a signed dot would read every one of them as pure grazing.
float fresnelReflectance(Surface s, vec3 V) {
    float f0 = dot(pbrF0(s), vec3(0.2126, 0.7152, 0.0722));
    float ndv = abs(dot(s.normal, V));
    return f0 + (1.0 - f0) * pow(1.0 - ndv, 5.0);
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

    // Transmission (glass): the share of the light that passes through the surface takes
    // the place of its diffuse term, and the scene behind it shows through instead
    // (KHR_materials_transmission).
    //
    // The map (red channel, per the extension) is what keeps a partly-glass object
    // from going uniformly transparent: without it a cabinet with glass panes turns
    // the whole cabinet into a ghost.
    float transmission = m.transmission;
    if ((m.flags & MAT_TRANS_MAP) != 0u) transmission *= tex(m.transMap, m.transSampler).r;
    vec3 V = normalize(pc.eye.xyz - vWorldPos);
    bool receives = (vFlags & FLAG_RECEIVES_SHADOW) != 0u;

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
    vec3 unfogged = shadeSurface(s, vWorldPos, V, pc.shadowSampler, 1.0 - transmission, specularScale, receives, indirect);
    if (transmission <= 0.0) {
        vec3 lit = applyFog(unfogged, vWorldPos, pc.eye.xyz, pc.lights.fogColor, pc.lights.fogParams);
        outColor = vec4(lit, outputAlpha(lit, unfogged, indirect, baseAlpha)); // linear: the target encodes it for display
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
    vec3 lit = applyFog(unfogged, vWorldPos, pc.eye.xyz, pc.lights.fogColor, pc.lights.fogParams);
    vec3 fogLight = applyFog(vec3(0.0), vWorldPos, pc.eye.xyz, pc.lights.fogColor, pc.lights.fogParams);

    if (pc.sceneCopy == NO_SCENE_COPY) {
        // No copy of the scene to read (see pix.sceneCopy): the scene behind shows
        // through by blending, neither bent nor tinted, and alpha is the share of it
        // the glass keeps out.
        float alpha = baseAlpha * (1.0 - passedShare);
        lit -= fogLight * (1.0 - alpha);
        outColor = vec4(lit, outputAlpha(lit, unfogged, indirect, alpha));
        return;
    }

    // With the scene copy the glass draws what is behind it itself, and covers the
    // scene in full where its colour's alpha does.
    lit -= fogLight * passedShare;
    lit += passedShare * sceneBehind(m, s, V);
    outColor = vec4(lit * baseAlpha, baseAlpha);
}
