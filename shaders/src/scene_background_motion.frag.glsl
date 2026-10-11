#version 460
#extension GL_EXT_scalar_block_layout : require

// How far the background moved since the previous frame, where the depth prepass drew
// nothing: the camera's motion alone, measured at the far plane. It runs in the
// prepass, after the geometry, over a full-screen triangle that passes the depth test
// only where nothing nearer was drawn.
//
// With an infinite far plane the point at depth 0 is at infinity, and the reprojection
// below handles it as one: a direction, which only the camera's turning moves.

layout(push_constant, scalar) uniform PC {
    // reprojection takes a point in this frame's clip space, without the jitter, to the
    // previous frame's; jitter is the clip-space offset this frame draws with, for TAA.
    mat4 reprojection;
    vec2 jitter;
    vec2 pad;
} pc;

layout(location = 0) in vec2 vUV;

layout(location = 0) out vec2 outMotion;

void main() {
    vec2 ndc = vUV * 2.0 - 1.0 - pc.jitter;
    vec4 previous = pc.reprojection * vec4(ndc, 0.0, 1.0);
    outMotion = (ndc - previous.xy / previous.w) * 0.5;
}
