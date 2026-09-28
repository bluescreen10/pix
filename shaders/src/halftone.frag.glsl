#version 460
#extension GL_EXT_nonuniform_qualifier : require
#extension GL_EXT_scalar_block_layout : require
#extension GL_GOOGLE_include_directive : require

// Halftone: redraw the image the way a printing press would, as a screen of ink dots on
// paper whose size carries the tone. In colour, four screens — cyan, magenta, yellow and
// black — each at its own angle, which is what gives print its rosette pattern.
//
// Dot size is chosen in display space: printed tone is perceptual, and a dot area
// proportional to linear light would leave every midtone looking far too light. The
// result is converted back to linear light, like every other pass's output.
#include "postfx.glsl"

layout(push_constant, scalar) uniform PC {
    POSTFX_ROOT
    float cellSize; // dot spacing, in pixels
    float angle;    // radians, added to every screen's angle
    uint style;     // mirrors postprocess.HalftoneStyle
    float pad0;
} pc;

layout(location = 0) out vec4 outColor;

const uint HALFTONE_CMYK = 0u;
const uint HALFTONE_MONO = 1u;

vec3 linearToSrgb(vec3 c) {
    c = clamp(c, 0.0, 1.0);
    return mix(1.055 * pow(c, vec3(1.0 / 2.4)) - 0.055, c * 12.92, lessThanEqual(c, vec3(0.0031308)));
}

vec3 srgbToLinear(vec3 c) {
    return mix(pow((c + 0.055) / 1.055, vec3(2.4)), c / 12.92, lessThanEqual(c, vec3(0.04045)));
}

// displayColor is the image at uv, as a display would show it.
vec3 displayColor(vec2 uv) {
    return linearToSrgb(sampleImage(pc.source, pc.linearSampler, uv).rgb);
}

// cmyk separates a display colour into ink amounts: cyan, magenta, yellow and black.
vec4 cmyk(vec3 rgb) {
    float k = 1.0 - max(rgb.r, max(rgb.g, rgb.b));
    vec3 cmy = (1.0 - rgb - k) / max(1.0 - k, 1e-4);
    return vec4(cmy, k);
}

// inkAmount is how much of one ink the image calls for at uv: one of CMYK's four, or
// for a mono print, how dark the image is there.
float inkAmount(vec2 uv, int ink) {
    vec3 rgb = displayColor(uv);
    if (pc.style == HALFTONE_MONO) {
        return 1.0 - dot(rgb, vec3(0.2126, 0.7152, 0.0722));
    }
    return cmyk(rgb)[ink];
}

// dotRadius is the radius, in cells, of a dot that inks the share amount of its cell.
// Area-true — pi r^2 = amount — until neighbouring dots touch at a quarter of pi; past
// that they merge, and grow to fill the cell's corners at full ink.
float dotRadius(float amount) {
    const float touching = 0.7853982;
    amount = clamp(amount, 0.0, 1.0);
    if (amount <= touching) {
        return sqrt(amount / 3.14159265);
    }
    return mix(0.5, 0.7072, (amount - touching) / (1.0 - touching));
}

// screen is how much of the pixel at p, in pixels, one screen's ink covers: 1 inside a
// dot, 0 outside, antialiased across the edge. The screen is a grid of cells rotated by
// screenAngle, each with one dot sized for the ink the image calls for at the cell's
// centre — not at p — so every dot is a clean disc.
//
// A dot past a quarter of pi of ink reaches into its neighbours' cells, so the pixel
// takes the most ink of the four dots around the cell corner nearest it: its own and
// the three that share that corner.
float screen(vec2 p, float screenAngle, int ink) {
    float c = cos(screenAngle);
    float s = sin(screenAngle);
    mat2 toScreen = mat2(c, -s, s, c);
    mat2 fromScreen = mat2(c, s, -s, c);

    vec2 q = toScreen * p / pc.cellSize;
    vec2 cell = floor(q);
    vec2 towardCorner = sign(fract(q) - 0.5);
    float edge = 0.5 / pc.cellSize; // half a pixel, in cells

    float coverage = 0.0;
    for (int i = 0; i < 4; i++) {
        vec2 centre = cell + 0.5 + vec2(i & 1, i >> 1) * towardCorner;
        vec2 centreUV = (fromScreen * (centre * pc.cellSize)) * pc.texelSize;
        float radius = dotRadius(inkAmount(centreUV, ink));
        float d = length(q - centre);
        // A dot smaller than a pixel fades out rather than leaving a speck: with no ink
        // there is no dot at all.
        float dotCoverage = (1.0 - smoothstep(radius - edge, radius + edge, d)) * min(radius / edge, 1.0);
        coverage = max(coverage, dotCoverage);
    }
    return coverage;
}

void main() {
    vec2 p = vUV / pc.texelSize;
    vec3 paper;
    if (pc.style == HALFTONE_MONO) {
        paper = vec3(1.0 - screen(p, radians(45.0) + pc.angle, 0));
    } else {
        // The classic print angles: cyan 15, magenta 75, yellow 0, black 45 degrees.
        float cyan = screen(p, radians(15.0) + pc.angle, 0);
        float magenta = screen(p, radians(75.0) + pc.angle, 1);
        float yellow = screen(p, radians(0.0) + pc.angle, 2);
        float black = screen(p, radians(45.0) + pc.angle, 3);
        // Inks are subtractive: each takes its complement out of the white paper.
        paper = (1.0 - vec3(cyan, magenta, yellow)) * (1.0 - black);
    }
    outColor = vec4(srgbToLinear(paper), 1.0);
}
