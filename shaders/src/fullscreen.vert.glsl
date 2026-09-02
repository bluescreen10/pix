#version 460

// Fullscreen triangle: 3 vertices, no vertex buffers, no push constants — a single
// oversized triangle that covers the whole viewport (the standard trick avoids the
// seam a two-triangle quad would need). Used by deferred lighting passes; the
// fragment shader derives screen position from gl_FragCoord, not from a varying here.
//
// Depth is pinned to the far clip value (1.0) deliberately: the deferred lighting
// pipeline binds the G-buffer depth read-only with CompareGreater, so a pixel passes
// only where the stored depth is nearer than the far plane — i.e. where geometry was
// actually drawn. Background is rejected by the hardware before the fragment shader.
void main() {
    vec2 p = vec2((gl_VertexIndex << 1) & 2, gl_VertexIndex & 2);
    gl_Position = vec4(p * 2.0 - 1.0, 1.0, 1.0);
}
