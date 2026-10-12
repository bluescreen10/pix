#version 460
#extension GL_EXT_nonuniform_qualifier : require
#extension GL_EXT_scalar_block_layout : require
#extension GL_EXT_samplerless_texture_functions : require
#extension GL_GOOGLE_include_directive : require

// Gathers the occlusion of one interleaved image of depth — the dispatch's z picks which
// — as ASSAO does at its lowest presets, but with the as many taps as the quality asks, 
// up to its High preset's twelve (see pc.taps): taps from the sample disk, each with its
// mirror through the centre, rotated and scaled by a matrix of the image's own, and
// rounded to whole texels so that a tap's depth is the depth at the position it is
// reconstructed at. Each tap obscures by how far it rises above the surface's tangent
// plane, past a small threshold, fading out towards the radius. Normals are reconstructed 
// from the neighbouring depths, as the VBAO pass does.
//
// It also finds which of the texel's neighbours lie on its surface, and stores them 
// packed beside the occlusion, for the  blur and the reassembly not to mix surfaces.
#include "assao.glsl"

layout(push_constant, scalar) uniform PC {
    vec4 pixelRay;
    vec4 pixelRayOrigin;
    uint depth[4];
    uint targets[4];
    int stride;
    float radius;
    float invRadiusNearLimit;
    float fadeOutMul;
    float fadeOutAdd;
    ivec2 interleavedSize;
    ivec2 fullSize;
    // taps is how many of SAMPLE_PATTERN's pairs of taps the gather takes, up to 12.
    int taps;
} pc;

// The first taps of ASSAO's sample disk: offset, weight, and log2 of the offset's length
// (unused here: no depth mips are read). A gather takes the first pc.taps of them.
// Low preset takes three; that leaves contour bands where occlusion falls off,
// as the disk grows and shrinks with depth and its few taps, rounded to whole texels,
// jump from texel to texel together. Twelve, its High preset's count, smooth them away.
const vec4 SAMPLE_PATTERN[12] = vec4[](
    vec4(0.78488064, 0.56661671, 1.500000, -0.126083), vec4(0.26022232, -0.29575172, 1.500000, -1.064030), vec4(0.10459357, 0.08372527, 1.110000, -2.730563), vec4(-0.68286800, 0.04963045, 1.090000, -0.498827),
    vec4(-0.13570161, -0.64190155, 1.250000, -0.532765), vec4(-0.26193795, -0.08205118, 0.670000, -1.783245), vec4(-0.61177456, 0.66664219, 0.710000, -0.044234), vec4(0.43675563, 0.25119025, 0.610000, -1.167283),
    vec4(0.07884444, 0.86618668, 0.640000, -0.459002), vec4(-0.12790935, -0.29869005, 0.600000, -1.729424), vec4(-0.04031125, 0.02413622, 0.600000, -4.792042), vec4(0.16201244, -0.52851415, 0.790000, -1.067055));

// How strong the occlusion is, the curve it is shaped by, how high a
// tap must rise above the tangent plane — as the cosine to the normal — to count, and
// the most any texel is occluded.
const float INTENSITY = 2.0;
const float SHADOW_POWER = 1.5;
const float HORIZON_ANGLE_THRESHOLD = 0.06;
const float SHADOW_CLAMP = 0.98;
// SAME_SURFACE is how far apart in depth, as a share of a texel's depth, it and a
// neighbour can be and still lie on one surface (see surfaceNormal).
const float SAME_SURFACE = 0.25;
const float PI = 3.14159265359;

float depthAtTexel(uint interleaved, ivec2 texel) {
    texel = clamp(texel, ivec2(0), pc.interleavedSize - 1);
    return texelFetch(gTextures[nonuniformEXT(pc.depth[interleaved])], texel, 0).r;
}

vec3 positionAt(uint interleaved, ivec2 texel, float viewDepth) {
    return viewPositionOnRay(interleavedPixel(texel, interleaved, pc.stride), viewDepth, pc.pixelRay, pc.pixelRayOrigin);
}

// rotationScale is the matrix interleaved image `interleaved` rotates and scales its taps
// by for one of five sub-patterns: twenty rotations spread evenly
// around a quarter turn, each a little larger than the last.
mat2 rotationScale(uint interleaved, uint subPattern) {
    const float SUB_PATTERNS = 5.0;
    const float SUB_PATTERN_ORDER[5] = float[](0.0, 1.0, 4.0, 3.0, 2.0);
    float a = float(interleaved);
    float b = SUB_PATTERN_ORDER[subPattern];
    float angle = (a + b / SUB_PATTERNS) * PI * 0.5;
    float scale = 1.0 + (a - 1.5 + (b - (SUB_PATTERNS - 1.0) * 0.5) / SUB_PATTERNS) * 0.07;
    float c = cos(angle);
    float s = sin(angle);
    return mat2(scale * c, scale * -s, -scale * s, -scale * c);
}

// surfaceNormal is the normal of the surface at texel, whose position is center, from
// its neighbours' positions, given their depths — left, right, top, bottom; each axis
// takes the neighbour nearer in depth, and a texel with no neighbour on its own surface
// along an axis is taken to face the eye.
vec3 surfaceNormal(uint interleaved, ivec2 texel, vec3 center, vec4 neighbourDepths) {
    float sameSurface = SAME_SURFACE * -center.z;
    vec3 across[2];
    for (int axis = 0; axis < 2; axis++) {
        ivec2 step = axis == 0 ? ivec2(1, 0) : ivec2(0, 1);
        float beforeDepth = axis == 0 ? neighbourDepths.x : neighbourDepths.z;
        float afterDepth = axis == 0 ? neighbourDepths.y : neighbourDepths.w;
        vec3 before = positionAt(interleaved, texel - step, beforeDepth);
        vec3 after = positionAt(interleaved, texel + step, afterDepth);
        float beforeGap = beforeDepth > 0.0 ? abs(before.z - center.z) : 1e30;
        float afterGap = afterDepth > 0.0 ? abs(after.z - center.z) : 1e30;
        if (min(beforeGap, afterGap) > sameSurface) {
            return normalize(-center);
        }
        across[axis] = afterGap <= beforeGap ? after - center : center - before;
    }
    vec3 normal = normalize(cross(across[0], across[1]));
    if (dot(normal, center) > 0.0) {
        normal = -normal;
    }
    return normal;
}

float pixelObscurance(vec3 normal, vec3 hitDelta, float falloffSquared) {
    float lengthSquared = dot(hitDelta, hitDelta);
    float cosine = dot(normal, hitDelta) * inversesqrt(lengthSquared);
    float falloff = max(0.0, lengthSquared * falloffSquared + 1.0);
    return max(0.0, cosine - HORIZON_ANGLE_THRESHOLD) * falloff;
}

layout(local_size_x = 8, local_size_y = 8) in;
void main() {
    ivec2 texel = ivec2(gl_GlobalInvocationID.xy);
    uint interleaved = gl_GlobalInvocationID.z;
    if (texel.x >= pc.interleavedSize.x || texel.y >= pc.interleavedSize.y) {
        return;
    }
    float depth = depthAtTexel(interleaved, texel);
    if (depth <= 0.0) {
        imageStore(gImages[nonuniformEXT(pc.targets[interleaved])], texel, vec4(1.0, packEdges(vec4(1.0)), 0.0, 0.0));
        return;
    }

    // Medium preset: which neighbours lie on this texel's surface, for the blur
    // and the reassembly to keep each surface's occlusion its own.
    vec4 neighbourDepths = vec4(
        depthAtTexel(interleaved, texel + ivec2(-1, 0)), depthAtTexel(interleaved, texel + ivec2(1, 0)),
        depthAtTexel(interleaved, texel + ivec2(0, -1)), depthAtTexel(interleaved, texel + ivec2(0, 1)));
    vec4 edges = surfaceEdges(depth, neighbourDepths.x, neighbourDepths.y, neighbourDepths.z, neighbourDepths.w);

    vec3 center = positionAt(interleaved, texel, depth);
    vec3 normal = surfaceNormal(interleaved, texel, center, neighbourDepths);
    // How large one texel of the image is in view space, at the centre's depth.
    float texelSize = positionAt(interleaved, texel + ivec2(1, 0), depth).x - center.x;

    // The radius, shrunk when the point is so close that the disk would cover more of
    // the screen than it is worth sampling, and fitted to the sampling pattern.
    float tooClose = clamp(length(center) * pc.invRadiusNearLimit, 0.0, 1.0) * 0.8 + 0.2;
    float radius = pc.radius * tooClose;
    float lookupRadius = (0.85 * radius) / abs(texelSize);
    float falloffSquared = -1.0 / (radius * radius);

    uint subPattern = uint(texel.y * 2 + texel.x) % 5u;
    mat2 rotation = rotationScale(interleaved, subPattern) * lookupRadius;
    // Moved a little towards the eye, against the depth's imprecision.
    vec3 from = center * 0.99;

    float obscuranceSum = 0.0;
    float weightSum = 0.0;
    for (int i = 0; i < pc.taps; i++) {
        vec2 offset = round(SAMPLE_PATTERN[i].xy * rotation);
        float weight = SAMPLE_PATTERN[i].z;
        for (int side = 0; side < 2; side++) {
            ivec2 tap = texel + ivec2(side == 0 ? offset : -offset);
            float tapDepth = depthAtTexel(interleaved, tap);
            if (tapDepth > 0.0) {
                vec3 hitDelta = positionAt(interleaved, clamp(tap, ivec2(0), pc.interleavedSize - 1), tapDepth) - from;
                obscuranceSum += pixelObscurance(normal, hitDelta, falloffSquared) * weight;
            }
            weightSum += weight;
        }
    }
    float obscurance = obscuranceSum / weightSum;
    float fadeOut = clamp(-center.z * pc.fadeOutMul + pc.fadeOutAdd, 0.0, 1.0);
    // A texel with edges on opposite sides lies on something too thin for its taps to
    // measure — a pillar a texel wide — and its occlusion fades, rather than flicker as
    // the thin thing moves across texels.
    float thinness = clamp((1.0 - edges.x - edges.y) * 0.35, 0.0, 1.0) + clamp((1.0 - edges.z - edges.w) * 0.35, 0.0, 1.0);
    fadeOut *= clamp(1.0 - thinness, 0.0, 1.0);
    obscurance = min(obscurance * INTENSITY, SHADOW_CLAMP) * fadeOut;
    float openness = pow(clamp(1.0 - obscurance, 0.0, 1.0), SHADOW_POWER);
    imageStore(gImages[nonuniformEXT(pc.targets[interleaved])], texel, vec4(openness, packEdges(edges), 0.0, 0.0));
}
