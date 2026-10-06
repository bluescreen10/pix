#version 460
#extension GL_EXT_nonuniform_qualifier : require
#extension GL_EXT_scalar_block_layout : require
#extension GL_EXT_samplerless_texture_functions : require
#extension GL_GOOGLE_include_directive : require

// Measures how open each texel's surface is, by visibility bitmask ambient occlusion
// (Therrien, Levesque and Gilet, "Screen Space Indirect Lighting with Visibility
// Bitmask", 2023), on the slices of ground-truth ambient occlusion as XeGTAO lays them out
// (XeGTAO_MainPass; Intel, MIT, github.com/GameTechDev/XeGTAO): pc.slices half-planes
// through the line of sight, turned about it by even steps from a start of the texel's
// own, and along each, pc.steps samples either way. Each sample hides the sectors of its
// slice between its front and its back, pc.thickness behind — so what is behind a thin
// thing, by more than that, stays in view. The share of the slice's light its open
// sectors hold, weighted by the cosine to the normal, is the slice's visibility; the
// slices' mean is the texel's.
//
// It measures every frame on its own, the same way every frame: no history, so nothing
// trails behind anything that moves. What its samples miss, the denoise pass that follows
// smooths over (see vbao_denoise.comp.glsl), which is why it also writes the edges
// between surfaces, beside the visibility. More slices and steps — a higher quality
// (see AmbientOcclusionQuality) — leave it less to smooth.
//
// The surface's normal is reconstructed from the depths around the texel, as XeGTAO's
// XE_GTAO_GENERATE_NORMALS_INPLACE does: pix keeps no normal buffer.
#include "occlusion.glsl"

layout(push_constant, scalar) uniform PC {
    vec4 pixelRay;
    vec4 pixelRayOrigin;
    uint chain;
    uint chainMips;
    uint top;
    uint target;
    ivec2 fullSize;
    float radius;
    // thickness is how deep every surface is taken to be, in world units.
    float thickness;
    // slices is how many slices each texel measures, and steps how many samples each
    // takes either way (see ambientOcclusionQualities in ambient_occlusion.go).
    float slices;
    float steps;
    float pad0;
    float pad1;
} pc;
// SAMPLE_DISTRIBUTION_POWER crowds the samples toward the centre, where small crevices
// are; FINAL_VALUE_POWER shapes the visibility the slices measure; DEPTH_MIP_OFFSET is
// how many doublings of a sample's distance, in texels, it reads the chain's first image
// for before it reads coarser ones. All three are XeGTAO's defaults.
const float SAMPLE_DISTRIBUTION_POWER = 2.0;
const float FINAL_VALUE_POWER = 1.5;
const float DEPTH_MIP_OFFSET = 3.3;
// SECTORS is how many sectors each slice's light is split into, one bit of a mask each:
// a sample marks those between its front and its back. Spaced by GTAO's weighting
// rather than by angle, each is an equal share of the light, and the mask's open share
// is the slice's visibility.
const uint SECTORS = 32u;
// PIXEL_TOO_CLOSE is how far, in texels, the first sample is pushed out at least: nearer,
// it would read the texel itself.
const float PIXEL_TOO_CLOSE = 1.3;
const float PI = 3.14159265359;
const float HALF_PI = 1.57079632679;

// depthNear reads the chain's mip at texel of its first image, clamped to its edges.
float depthNear(ivec2 texel, int mip) {
    ivec2 size = mipSize(pc.fullSize, int(pc.top) + mip);
    return texelFetch(gTextures[nonuniformEXT(pc.chain)], clamp(texel >> mip, ivec2(0), size - 1), mip).r;
}

// sampleAt is the view-space position of what a sample at texel of the chain's first
// image reads from mip: the depth of the mip's texel there, at the position that texel
// stands for, as the mip's depth is the surface's there (see
// vbao_depth_mip.comp.glsl). XeGTAO places the depth at the sample's own texel
// instead; on a floor seen at a slant the two differ by the slope across a coarse texel,
// and the floor occludes itself, in rings where samples cross from one mip to the next.
vec3 sampleAt(ivec2 texel, int mip) {
    int top = int(pc.top);
    ivec2 size = mipSize(pc.fullSize, top + mip);
    ivec2 mipTexel = clamp(texel >> mip, ivec2(0), size - 1);
    float depth = texelFetch(gTextures[nonuniformEXT(pc.chain)], mipTexel, mip).r;
    return viewPositionOnRay(texelPixel(mipTexel, top + mip, top), depth, pc.pixelRay, pc.pixelRayOrigin);
}

// positionAt is the view-space position of the surface at texel of the chain's first
// image, viewDepth in front of the eye.
vec3 positionAt(ivec2 texel, float viewDepth) {
    int top = int(pc.top);
    return viewPositionOnRay(texelPixel(texel, top, top), viewDepth, pc.pixelRay, pc.pixelRayOrigin);
}

// isOnImage reports whether texel lies on an image size texels across. A sample off it
// would read the image's edge, again and again as its slice runs on, and find an
// occluder there that is nowhere near: a dark rim round the frame.
bool isOnImage(ivec2 texel, ivec2 size) {
    return all(greaterThanEqual(texel, ivec2(0))) && all(lessThan(texel, size));
}

// texelEdges says, for each of a texel's left, right, top and bottom neighbours, how
// much it lies on the texel's surface: 1 on it, 0 across an edge (XeGTAO_CalculateEdges).
// A neighbour's depth differs by the surface's slope as well as by any edge; what the
// slope across the texel accounts for is no edge.
vec4 texelEdges(float center, vec4 neighbours) {
    vec4 differences = neighbours - center;
    float slopeAcross = (differences.y - differences.x) * 0.5;
    float slopeDown = (differences.w - differences.z) * 0.5;
    vec4 slopeAdjusted = differences + vec4(slopeAcross, -slopeAcross, slopeDown, -slopeDown);
    differences = min(abs(differences), abs(slopeAdjusted));
    return clamp(1.25 - differences / (center * 0.011), 0.0, 1.0);
}

// surfaceNormal is the normal at center from its neighbours' positions — left, right,
// top, bottom — each corner's cross product counting as much as both its sides lie on
// the surface (XeGTAO_CalculateNormal).
vec3 surfaceNormal(vec4 edges, vec3 center, vec3 left, vec3 right, vec3 top, vec3 bottom) {
    vec4 accepted = clamp(vec4(edges.x * edges.z, edges.z * edges.y, edges.y * edges.w, edges.w * edges.x) + 0.01, 0.0, 1.0);
    left = normalize(left - center);
    right = normalize(right - center);
    top = normalize(top - center);
    bottom = normalize(bottom - center);
    // The eye looks down -z here and down +z in XeGTAO, so each corner's cross product
    // is taken the other way round from its, for the normal to face the eye.
    vec3 normal = accepted.x * cross(top, left) + accepted.y * cross(right, top) +
        accepted.z * cross(bottom, right) + accepted.w * cross(left, bottom);
    return normalize(normal);
}

// hilbertIndex is texel's place along a Hilbert curve filling a 64x64 tile.
uint hilbertIndex(uvec2 texel) {
    const uint WIDTH = 64u;
    texel &= WIDTH - 1u;
    uint index = 0u;
    for (uint level = WIDTH / 2u; level > 0u; level /= 2u) {
        uint regionX = (texel.x & level) > 0u ? 1u : 0u;
        uint regionY = (texel.y & level) > 0u ? 1u : 0u;
        index += level * level * ((3u * regionX) ^ regionY);
        if (regionY == 0u) {
            if (regionX == 1u) {
                texel = uvec2(WIDTH - 1u) - texel;
            }
            texel = texel.yx;
        }
    }
    return index;
}

// texelNoise is where texel starts its slices and its samples along them: Martin
// Roberts's R2 sequence, stepped along the Hilbert curve, so that neighbouring texels
// start far apart and every start is near some texel's (XeGTAO's SpatioTemporalNoise,
// without its temporal part).
vec2 texelNoise(ivec2 texel) {
    float index = float(hilbertIndex(uvec2(texel)));
    return fract(0.5 + index * vec2(0.75487766624669276005, 0.5698402909980532659114));
}

// sliceShare is how much of the light a slice lets through, from the tangent plane on
// the far side of the eye up to angle — measured from the eye, positive toward the
// slice's direction — as a share of all it lets through: 0 at that tangent plane, 1 at
// the other. The light is weighted as GTAO weighs it, by the cosine to the normal and
// the slice's own width at each angle, cos(a - n)·|sin a|, so that sectors of equal
// share are equal in light: cos n + n sin n is the whole of it.
float sliceShare(float angle, float n, float sinN, float cosN, float open) {
    angle = clamp(angle, n - HALF_PI, n + HALF_PI);
    // The integral of cos(a - n)·sin a is (2a sin n - cos(2a - n)) / 4; |sin a| turns
    // it round where the angle crosses the eye.
    float atStart = (2.0 * (n - HALF_PI) * sinN + cosN) / 4.0;
    float atAngle = (2.0 * angle * sinN - cos(2.0 * angle - n)) / 4.0;
    float upToAngle = angle <= 0.0 ? atStart - atAngle : atStart + atAngle + cosN / 2.0;
    return clamp(upToAngle / open, 0.0, 1.0);
}

// sectorsBetween marks the sectors from share from to share to of a slice's light: those
// it covers at least half of. Rounding outward instead, a sample on a flat surface, on
// its tangent plane but for acos's error, marked the sector at the edge of the slice,
// and an open floor read 2 of 32 occluded.
uint sectorsBetween(float from, float to) {
    uint first = uint(round(from * float(SECTORS)));
    uint end = uint(round(to * float(SECTORS)));
    if (end <= first) {
        return 0u;
    }
    uint count = end - first;
    uint run = count >= SECTORS ? 0xFFFFFFFFu : (1u << count) - 1u;
    return run << first;
}


// fastAcos is acos to within about 0.01, from [-1, 1] to [0, PI].
float fastAcos(float x) {
    float magnitude = abs(x);
    float angle = (-0.156583 * magnitude + HALF_PI) * sqrt(1.0 - magnitude);
    return x >= 0.0 ? angle : PI - angle;
}

layout(local_size_x = 8, local_size_y = 8) in;
void main() {
    ivec2 texel = ivec2(gl_GlobalInvocationID.xy);
    int top = int(pc.top);
    ivec2 size = mipSize(pc.fullSize, top);
    if (texel.x >= size.x || texel.y >= size.y) {
        return;
    }

    float centerDepth = depthNear(texel, 0);
    vec4 neighbourDepths = vec4(depthNear(texel + ivec2(-1, 0), 0), depthNear(texel + ivec2(1, 0), 0),
        depthNear(texel + ivec2(0, -1), 0), depthNear(texel + ivec2(0, 1), 0));
    vec4 edges = texelEdges(centerDepth, neighbourDepths);
    if (centerDepth >= BACKGROUND_DEPTH) {
        imageStore(gImages[nonuniformEXT(pc.target)], texel, vec4(1.0, packEdges(vec4(1.0)), 0.0, 0.0));
        return;
    }
    vec3 normal = surfaceNormal(edges, positionAt(texel, centerDepth),
        positionAt(texel + ivec2(-1, 0), neighbourDepths.x), positionAt(texel + ivec2(1, 0), neighbourDepths.y),
        positionAt(texel + ivec2(0, -1), neighbourDepths.z), positionAt(texel + ivec2(0, 1), neighbourDepths.w));

    // Moved a hair toward the eye, against the depth's imprecision.
    centerDepth *= 0.99999;
    vec3 center = positionAt(texel, centerDepth);
    vec3 toEye = normalize(-center);

    float effectRadius = pc.radius * RADIUS_MULTIPLIER;
    float falloffRange = FALLOFF_RANGE * effectRadius;
    float falloffFrom = effectRadius * (1.0 - FALLOFF_RANGE);
    // A sample counts as far as the middle of XeGTAO's fade, where it would count half.
    float reach = falloffFrom + falloffRange * 0.5;

    // The radius in texels, from how wide a texel is at the centre's depth.
    float texelWidth = abs(positionAt(texel + ivec2(1, 0), centerDepth).x - center.x);
    float screenRadius = effectRadius / texelWidth;
    // A radius of a few texels is too small to measure; it fades to open instead.
    float smallRadiusFade = clamp((10.0 - screenRadius) / 100.0, 0.0, 1.0) * 0.5;
    float visibility = 0.0;
    float unoccluded = 0.0;
    float minStep = PIXEL_TOO_CLOSE / screenRadius;

    vec2 noise = texelNoise(texel);
    int lastMip = int(pc.chainMips) - 1;
    for (float slice = 0.0; slice < pc.slices; slice += 1.0) {
        float phi = (slice + noise.x) / pc.slices * PI;
        vec2 screenDirection = vec2(cos(phi), sin(phi));
        vec2 omega = screenDirection * screenRadius;
        // The slice's direction in view space: where a texel along it lands at the
        // centre's depth. Taken from the projection rather than worked out by hand, so
        // the side marched on screen and the side the normal is measured on cannot
        // disagree.
        vec3 direction = normalize(viewPositionOnRay(texelPixel(texel, top, top) + screenDirection, centerDepth, pc.pixelRay, pc.pixelRayOrigin) - center);
        vec3 orthoDirection = direction - dot(direction, toEye) * toEye;
        vec3 axis = normalize(cross(orthoDirection, toEye));
        vec3 projectedNormal = normal - axis * dot(normal, axis);
        float signNormal = sign(dot(orthoDirection, projectedNormal));
        float projectedLength = length(projectedNormal);
        float cosNormal = clamp(dot(projectedNormal, toEye) / projectedLength, 0.0, 1.0);
        float n = signNormal * fastAcos(cosNormal);

        float sinN = sin(n);
        // What the slice lets through with nothing in the way.
        float open = cosNormal + n * sinN;
        uint occluded = 0u;
        for (float step = 0.0; step < pc.steps; step += 1.0) {
            // R1 sequence along the slice, from the texel's start.
            float stepNoise = fract(noise.y + (slice + step * pc.steps) * 0.6180339887498948482);
            float s = pow((step + stepNoise) / pc.steps, SAMPLE_DISTRIBUTION_POWER) + minStep;
            vec2 offset = s * omega;
            int mip = clamp(int(round(log2(length(offset)) - DEPTH_MIP_OFFSET)), 0, lastMip);
            // Whole texels, for each sample's depth to be read where its position is.
            ivec2 step0 = ivec2(round(offset));
            for (int side = 0; side < 2; side++) {
                ivec2 sampleTexel = side == 0 ? texel + step0 : texel - step0;
                vec3 delta = sampleAt(sampleTexel, mip) - center;
                // Past the radius, or off the image, a sample occludes nothing.
                bool isNear = length(delta) < reach && isOnImage(sampleTexel, size);
                if (!isNear) {
                    continue;
                }
                // Rounded to a whole texel, a sample lies off the slice by up to half
                // of one; it is measured by its place within the slice.
                delta -= axis * dot(delta, axis);
                // Its front, and its back pc.thickness behind it, seen from the centre, as
                // angles from the eye — toward the slice's direction for the first
                // side, away from it for the second.
                vec3 back = delta - toEye * pc.thickness;
                float sideSign = side == 0 ? 1.0 : -1.0;
                float frontAngle = sideSign * fastAcos(dot(normalize(delta), toEye));
                float backAngle = sideSign * fastAcos(dot(normalize(back), toEye));
                float from = sliceShare(min(frontAngle, backAngle), n, sinN, cosNormal, open);
                float to = sliceShare(max(frontAngle, backAngle), n, sinN, cosNormal, open);
                occluded |= sectorsBetween(from, to);
            }
        }

        // XeGTAO's fudge for a slight overdarkening on steep slopes.
        projectedLength = mix(projectedLength, 1.0, 0.05);
        float seen = 1.0 - float(bitCount(occluded)) / float(SECTORS);
        visibility += projectedLength * open * seen;
        unoccluded += projectedLength * open;
    }
    // The visibility as a share of what the slices would see with nothing in the way,
    // rather than of the whole sky as XeGTAO takes it: three slices even on screen are
    // not even about the line of sight away from the image's centre, and an open floor
    // there read 0.95 rather than 1.
    visibility = visibility / max(unoccluded, 1e-6) + smallRadiusFade;
    visibility = max(pow(clamp(visibility, 0.0, 1.0), FINAL_VALUE_POWER), 0.03);
    imageStore(gImages[nonuniformEXT(pc.target)], texel, vec4(visibility, packEdges(edges), 0.0, 0.0));
}
