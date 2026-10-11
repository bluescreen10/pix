#version 460
#extension GL_GOOGLE_include_directive : require

// The depth prepass's fragment stage while motion vectors are on, for unmasked
// materials: it writes how far the surface moved since the previous frame (see
// pix.Frame.SceneMotion). Without motion vectors the prepass has no fragment stage.
#include "motion.glsl"

layout(location = 3) in vec4 vClip;
layout(location = 4) in vec4 vPreviousClip;

layout(location = 0) out vec2 outMotion;

void main() {
    outMotion = screenMotion(vClip, vPreviousClip);
}
