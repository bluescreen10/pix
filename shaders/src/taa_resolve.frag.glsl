#version 460
#extension GL_EXT_nonuniform_qualifier : require
#extension GL_EXT_scalar_block_layout : require
#extension GL_EXT_samplerless_texture_functions : require
#extension GL_GOOGLE_include_directive : require

// TAA's resolve (see pix.AntiAliasingTAA): blends this frame, drawn from one spot within
// each pixel, into the history of the frames before it, so that over frames each pixel
// averages the whole of itself.
//
// The history is read where the pixel's surface was last frame — its texture coordinate
// minus its motion — with a Catmull-Rom filter, which keeps it sharp where a bilinear
// read would soften it a little more every frame. What it holds there is only trusted
// as far as this frame agrees: it is clipped to the range of colours around the pixel
// now, which is what keeps what moved or was uncovered from trailing behind.
//
// Motion is taken from the nearest surface around the pixel, not the pixel itself: at an
// edge, a background pixel next to a moving object would otherwise keep the history the
// object left behind.
//
// Colours are blended compressed, c / (1 + max(c)), so that one very bright sample — a
// glint caught on one frame and not the next — does not dominate the average and
// flicker.
#include "postfx.glsl"

layout(push_constant, scalar) uniform PC {
    POSTFX_ROOT
    // history is the bindless index of the previous frame's resolve, and motion of the
    // frame's motion (see pix.Frame.SceneMotion). hasHistory is 0 on the first frame,
    // which has nothing to blend with.
    uint history;
    uint motion;
    uint hasHistory;
    uint pad;
} pc;

layout(location = 0) out vec4 outColor;

// currentWeight is how much of each frame the history takes in: over about 1/weight
// frames, a pixel converges on its average.
const float currentWeight = 0.1;

vec4 fetchColor(ivec2 texel) {
    return texelFetch(gTextures[nonuniformEXT(pc.source)], texel, 0);
}

float fetchDepth(ivec2 texel) {
    return texelFetch(gTextures[nonuniformEXT(pc.sceneDepth)], texel, 0).r;
}

vec2 fetchMotion(ivec2 texel) {
    return texelFetch(gTextures[nonuniformEXT(pc.motion)], texel, 0).rg;
}

vec3 sampleHistory(vec2 uv) {
    return sampleImage(pc.history, pc.linearSampler, uv).rgb;
}

vec3 compress(vec3 color) {
    return color / (1.0 + max(color.r, max(color.g, color.b)));
}

vec3 uncompress(vec3 color) {
    return color / (1.0 - min(max(color.r, max(color.g, color.b)), 0.999));
}

vec3 toYCoCg(vec3 rgb) {
    return vec3(dot(rgb, vec3(0.25, 0.5, 0.25)), dot(rgb, vec3(0.5, 0.0, -0.5)), dot(rgb, vec3(-0.25, 0.5, -0.25)));
}

vec3 fromYCoCg(vec3 yCoCg) {
    return vec3(yCoCg.x + yCoCg.y - yCoCg.z, yCoCg.x + yCoCg.z, yCoCg.x - yCoCg.y - yCoCg.z);
}

// historyCatmullRom reads the history at uv through a Catmull-Rom filter, in five
// bilinear reads rather than sixteen point ones: the inner two by two taps of each axis
// share one read placed between them, and the four corners, whose weights are smallest,
// are left out.
vec3 historyCatmullRom(vec2 uv) {
    vec2 size = 1.0 / pc.texelSize;
    vec2 position = uv * size;
    vec2 center = floor(position - 0.5) + 0.5;
    vec2 f = position - center;

    vec2 w0 = f * (-0.5 + f * (1.0 - 0.5 * f));
    vec2 w1 = 1.0 + f * f * (-2.5 + 1.5 * f);
    vec2 w2 = f * (0.5 + f * (2.0 - 1.5 * f));
    vec2 w3 = f * f * (-0.5 + 0.5 * f);
    vec2 w12 = w1 + w2;

    vec2 uv0 = (center - 1.0) * pc.texelSize;
    vec2 uv3 = (center + 2.0) * pc.texelSize;
    vec2 uv12 = (center + w2 / w12) * pc.texelSize;

    vec3 sum = sampleHistory(vec2(uv12.x, uv0.y)) * (w12.x * w0.y)
             + sampleHistory(vec2(uv0.x, uv12.y)) * (w0.x * w12.y)
             + sampleHistory(uv12) * (w12.x * w12.y)
             + sampleHistory(vec2(uv3.x, uv12.y)) * (w3.x * w12.y)
             + sampleHistory(vec2(uv12.x, uv3.y)) * (w12.x * w3.y);
    float weight = w12.x * w0.y + w0.x * w12.y + w12.x * w12.y + w3.x * w12.y + w12.x * w3.y;
    return max(sum / weight, vec3(0.0));
}

// clipToBox moves history toward the middle of the box until it lies within it.
vec3 clipToBox(vec3 history, vec3 boxMin, vec3 boxMax) {
    vec3 center = 0.5 * (boxMax + boxMin);
    vec3 extent = 0.5 * (boxMax - boxMin) + 1e-5;
    vec3 offset = history - center;
    vec3 units = abs(offset / extent);
    float farthest = max(units.x, max(units.y, units.z));
    return farthest > 1.0 ? center + offset / farthest : history;
}

void main() {
    ivec2 size = textureSize(gTextures[nonuniformEXT(pc.source)], 0);
    ivec2 pixel = ivec2(gl_FragCoord.xy);
    vec4 current = fetchColor(pixel);

    vec3 boxMin = vec3(1e9);
    vec3 boxMax = vec3(-1e9);
    ivec2 nearest = pixel;
    float nearestDepth = -1.0;
    for (int y = -1; y <= 1; y++) {
        for (int x = -1; x <= 1; x++) {
            ivec2 texel = clamp(pixel + ivec2(x, y), ivec2(0), size - 1);
            vec3 color = toYCoCg(compress(fetchColor(texel).rgb));
            boxMin = min(boxMin, color);
            boxMax = max(boxMax, color);
            // Depth is reversed: the nearest surface has the greatest.
            float depth = fetchDepth(texel);
            if (depth > nearestDepth) {
                nearestDepth = depth;
                nearest = texel;
            }
        }
    }

    vec2 historyUV = vUV - fetchMotion(nearest);
    bool isHistoryOnScreen = all(greaterThanEqual(historyUV, vec2(0.0))) && all(lessThanEqual(historyUV, vec2(1.0)));
    if (pc.hasHistory == 0u || !isHistoryOnScreen) {
        outColor = current;
        return;
    }

    vec3 history = clipToBox(toYCoCg(compress(historyCatmullRom(historyUV))), boxMin, boxMax);
    vec3 blended = mix(history, toYCoCg(compress(current.rgb)), currentWeight);
    outColor = vec4(uncompress(fromYCoCg(blended)), current.a);
}
