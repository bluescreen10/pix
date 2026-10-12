#version 460

// One triangle that covers the whole target, with no vertex buffer: three vertices at
// (-1,-1), (3,-1) and (-1,3) in clip space, the part outside the viewport clipped away.
// vUV runs 0..1 across the visible part, top-left at (0,0) — the same orientation the
// render targets are sampled in.
layout(location = 0) out vec2 vUV;

void main() {
    vec2 uv = vec2((gl_VertexIndex << 1) & 2, gl_VertexIndex & 2);
    vUV = uv;
    gl_Position = vec4(uv * 2.0 - 1.0, 0.0, 1.0);
}
