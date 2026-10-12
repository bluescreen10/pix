// motion.glsl — how far a surface moved on screen since the previous frame, from where
// the vertex stage projected it this frame and last (see scene_depth.vert's USE_MOTION).
#ifndef PIX_MOTION_GLSL
#define PIX_MOTION_GLSL

// screenMotion is the motion in screen widths and heights, rightward and downward:
// both matrices have clip-space Y flipped, so NDC Y already runs down the image.
//
// Both clip positions arrive interpolated and are divided here, per fragment, rather
// than divided in the vertex stage: a perspective division does not interpolate
// linearly.
vec2 screenMotion(vec4 clip, vec4 previousClip) {
    return (clip.xy / clip.w - previousClip.xy / previousClip.w) * 0.5;
}

#endif
