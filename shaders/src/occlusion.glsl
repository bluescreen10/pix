// occlusion.glsl — what the ambient occlusion passes share.
//
// GTAO works on a chain of the scene's depth, as XeGTAO does (Intel's GTAO, MIT, at
// github.com/GameTechDev/XeGTAO): each texel the distance in front of the eye, the first
// image at the resolution occlusion is measured at, full or half, and each after it half
// the one before. Where nothing was drawn it holds BACKGROUND_DEPTH, as far as anything
// can be, so the sky occludes nothing.
//
// The first image keeps the depth of the top-left pixel of each block it covers, and
// each texel stands for that pixel's centre (see texelPixel): a real pixel's depth at
// that pixel's position. Averaging a block would invent a surface halfway across any
// edge in it, and picking a different child from texel to texel, as Scalable Ambient
// Obscurance does, makes the image crawl with every sub-pixel move of the camera.
#ifndef PIX_OCCLUSION_GLSL
#define PIX_OCCLUSION_GLSL

#include "bindless.glsl"

// BACKGROUND_DEPTH is the depth chain's depth where no surface is: the largest half
// float, as XeGTAO clamps its depths to.
const float BACKGROUND_DEPTH = 65504.0;

// depthAt reads the depth at texel of one mip of the chain, whose first image is mip top.
float depthAt(uint chain, ivec2 texel, int mip, int top) {
    return texelFetch(gTextures[nonuniformEXT(chain)], texel, mip - top).r;
}

// texelPixel is the full-resolution position texel of mip stands for, in a chain whose
// first image is mip top: the centre of the top-left pixel of its block for the first
// image, and for a coarser one the centre of the positions its children stand for.
vec2 texelPixel(ivec2 texel, int mip, int top) {
    return vec2(texel << mip) + 0.5 + float((1 << mip) - (1 << top)) / 2.0;
}

// mipSize is the size of a mip of an image fullSize pixels across.
ivec2 mipSize(ivec2 fullSize, int mip) {
    return max(fullSize >> mip, ivec2(1));
}

// combinePair is one depth for two taken either side of a point, the same distance off:
// their average where they agree, as one surface's depths a pixel apart do, and
// otherwise the nearer.
float combinePair(float first, float second) {
    if (first <= 0.0 || second <= 0.0) {
        return max(first, second);
    }
    float nearest = max(first, second);
    if (abs(first - second) > 0.05 * nearest) {
        return nearest;
    }
    return (first + second) / 2.0;
}

// linearViewDepth turns a depth buffer's value into how far in front of the eye it
// lies, by the rows of the inverse projection that give view z and w (see
// depthUnprojection in ambient_occlusion.go). 0, where no surface is, stays 0.
float linearViewDepth(float depth, vec4 unprojection) {
    if (depth <= 0.0) {
        return 0.0;
    }
    float z = unprojection.x * depth + unprojection.y;
    float w = unprojection.z * depth + unprojection.w;
    return -z / w;
}

// sceneDepthAt reads the scene's depth buffer at the centre of pixel. A multisampled one
// holds no depth at the centre, but the first and last of its samples lie on either side
// of it, the same distance off — in the standard 2x and 4x patterns, which every backend
// draws with — so where both are on one surface their average is its depth at the
// centre.
float sceneDepthAt(uint depth, uint samples, ivec2 pixel) {
    if (samples <= 1u) {
        return texelFetch(gTextures[nonuniformEXT(depth)], pixel, 0).r;
    }
    float first = texelFetch(gTexturesMS[nonuniformEXT(depth)], pixel, 0).r;
    float last = texelFetch(gTexturesMS[nonuniformEXT(depth)], pixel, int(samples) - 1).r;
    return combinePair(first, last);
}

// viewPositionOnRay is the view-space position of the surface seen through pixel — a
// position in full-resolution pixels, the top-left corner at 0 — whose view depth is
// viewDepth, from the pixel's ray through view space: pixelRay and pixelRayOrigin (see
// pixelRays in ambient_occlusion.go) give, linearly from the pixel, the view x and y the
// ray moves per unit of view z, and where it is at z = 0 — the eye for a perspective
// camera, the pixel's own place for an orthographic one. Two multiply-adds a position,
// for passes that place many taps a texel.
vec3 viewPositionOnRay(vec2 pixel, float viewDepth, vec4 pixelRay, vec4 pixelRayOrigin) {
    float z = -viewDepth;
    vec2 direction = pixelRay.xy * pixel + pixelRay.zw;
    vec2 origin = pixelRayOrigin.xy * pixel + pixelRayOrigin.zw;
    return vec3(origin + direction * z, z);
}

// packEdges keeps four edge values — how much each of a texel's left, right, top and
// bottom neighbours lies on its surface, 1 on it and 0 across an edge — in one 8-bit
// value, two bits each from the highest down, for them to ride beside the occlusion they
// shape: four levels each are enough for weights, and keep transitions smooth.
float packEdges(vec4 edges) {
    edges = round(clamp(edges, 0.0, 1.0) * 3.05);
    return dot(edges, vec4(64.0 / 255.0, 16.0 / 255.0, 4.0 / 255.0, 1.0 / 255.0));
}

// XeGTAO's tuning, fitted to ray-traced ground truth: the radius occlusion is measured
// over is RADIUS_MULTIPLIER times the one asked for, and occluders fade out over the
// outer FALLOFF_RANGE share of it.
const float RADIUS_MULTIPLIER = 1.457;
const float FALLOFF_RANGE = 0.615;

#endif
