#version 460
#extension GL_EXT_nonuniform_qualifier : require
#extension GL_EXT_scalar_block_layout : require
#extension GL_GOOGLE_include_directive : require

// The motion debug view: the frame's measured motion (pix.Frame.SceneMotion), in pixels,
// rightward in red and downward in green, as 0.5 + motion/64: mid-grey is still, and
// either colour runs out at 32 pixels a frame.
//
// In pixels, not the screen widths the motion is stored in: a frame's motion is a few
// pixels, which in widths of a large frame is under one step of an 8-bit colour, and the
// view would be one flat grey.
#include "postfx.glsl"

layout(push_constant, scalar) uniform PC {
    POSTFX_ROOT
} pc;

layout(location = 0) out vec4 outColor;

// pixelsAcrossView is the motion from one end of either colour to the other.
const float pixelsAcrossView = 64.0;

// srgbToLinear undoes the encoding the display target applies, so that what it stores is
// the view's value itself, an even step per pixel of motion.
vec3 srgbToLinear(vec3 c) {
    return mix(pow((c + 0.055) / 1.055, vec3(2.4)), c / 12.92, lessThanEqual(c, vec3(0.04045)));
}

void main() {
    vec2 motionPixels = sampleImage(pc.source, pc.linearSampler, vUV).rg / pc.texelSize;
    vec2 shown = clamp(0.5 + motionPixels / pixelsAcrossView, 0.0, 1.0);
    outColor = vec4(srgbToLinear(vec3(shown, 0.5)), 1.0);
}
