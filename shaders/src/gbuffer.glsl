// gbuffer.glsl — the deferred-rendering SDK: the Surface a material's deferred pass
// fills, and the pack/unpack helpers that translate it to/from the concrete G-buffer
// layout. A material author never touches raw G-buffer channels — only Surface — so
// the layout can change here without touching any material's shader.
//
// Layout (4 targets, 16 bytes/pixel):
//
//   diffuse   RGBA8   rgb = albedo,   a = shading-model id
//   normal    RG16F   octahedral world normal
//   material  RGBA8   MODEL-DEFINED parameters (see below)
//   emissive  RGBA8   rgb = emissive, a = free
//
// Position is not stored — it is reconstructed from the depth buffer (worldFromDepth).
//
// Albedo/normal/emissive are universal: every shading model means the same thing by
// them. `material` deliberately is NOT — it is a scratch block whose interpretation
// belongs to the shading model that wrote it, identified by diffuse.a. Metallic-
// roughness PBR packs (metallic, roughness, ao); a Blinn-Phong model would pack
// (specular, shininess, …); a toon model might pack a ramp index. A model's deferred
// pass and its lighting pass are written together and are the only two shaders that
// agree on the meaning, so nothing else needs to care — which is what lets a
// user-defined material join the G-buffer without touching the engine.
#ifndef PIX_GBUFFER_GLSL
#define PIX_GBUFFER_GLSL

// Surface is the material-independent shading input: everything a lighting pass needs
// to shade a pixel, minus position. Emissive rides in the G-buffer so the lighting pass
// can add it to the lit result and sRGB-encode the sum ONCE — encoding emissive and
// lighting separately and blending them is visibly wrong (srgb(a)+srgb(b) is far
// brighter than srgb(a+b); two 0.25 terms already clip to white).
struct Surface {
    vec3 albedo;
    vec3 normal;   // world-space, normalized
    vec3 emissive;
    vec4 material; // model-defined; see the layout note above
    uint model;    // shading-model id, so a lighting pass can read `material` correctly
};

// GBufferOut is what packSurface produces; the caller (a deferred pass) assigns these
// to its own locally-declared color outputs, so shaders that only unpack — a lighting
// pass — don't declare render targets they never write.
struct GBufferOut {
    vec4 diffuse;
    vec2 normal;
    vec4 material;
    vec4 emissive;
};

// encodeOct/decodeOct: octahedral normal encoding (Cigolle et al.) — a unit vector
// packed into two components, good precision for a G-buffer normal target.
vec2 encodeOct(vec3 n) {
    vec2 p = n.xy * (1.0 / (abs(n.x) + abs(n.y) + abs(n.z)));
    if (n.z < 0.0) {
        vec2 signs = vec2(p.x >= 0.0 ? 1.0 : -1.0, p.y >= 0.0 ? 1.0 : -1.0);
        p = (1.0 - abs(p.yx)) * signs;
    }
    return p;
}

vec3 decodeOct(vec2 e) {
    vec3 n = vec3(e.xy, 1.0 - abs(e.x) - abs(e.y));
    float t = max(-n.z, 0.0);
    n.x += n.x >= 0.0 ? -t : t;
    n.y += n.y >= 0.0 ? -t : t;
    return normalize(n);
}

// The model id must round-trip exactly through diffuse.a (UNORM8), so keep the number
// of deferred shading models well under 255.
GBufferOut packSurface(Surface s) {
    GBufferOut o;
    o.diffuse = vec4(s.albedo, float(s.model) / 255.0);
    o.normal = encodeOct(normalize(s.normal));
    o.material = s.material;
    o.emissive = vec4(s.emissive, 0.0); // linear; lighting encodes the final sum
    return o;
}

Surface unpackGBuffer(vec4 diffuse, vec2 normal, vec4 material, vec4 emissive) {
    Surface s;
    s.albedo = diffuse.rgb;
    s.model = uint(diffuse.a * 255.0 + 0.5);
    s.normal = decodeOct(normal);
    s.material = material;
    s.emissive = emissive.rgb;
    return s;
}

// worldFromDepth reconstructs a world-space position from a depth sample using the
// SAME (already Y-flipped) view-projection matrix the G-buffer pass rendered with.
// ndc.xy comes from the fragment's raster position via the standard Vulkan viewport
// mapping (ndc in [-1,1] -> screen in [0,size]), so inverting that exact matrix is
// self-consistent regardless of which Y convention it encodes.
vec3 worldFromDepth(vec2 fragCoord, vec2 screenSize, float depth, mat4 invViewProj) {
    vec2 ndc = (fragCoord / screenSize) * 2.0 - 1.0;
    vec4 clip = vec4(ndc, depth, 1.0);
    vec4 world = invViewProj * clip;
    return world.xyz / world.w;
}

#endif // PIX_GBUFFER_GLSL
