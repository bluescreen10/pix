#version 460

// Fullscreen triangle: 3 vertices, no vertex buffers, no push constants — a single
// oversized triangle that covers the whole viewport (the standard trick avoids the
// seam a two-triangle quad would need). Used by deferred lighting passes; the
// fragment shader derives screen position from gl_FragCoord, not from a varying here.
//
// Depth is pinned to the far clip value deliberately, which under the engine's
// REVERSED-Z convention is 0.0, not 1.0 (see glm.PerspectiveRevZRH). The deferred
// lighting pipeline binds the G-buffer depth read-only with CompareLess, so a pixel
// passes only where the stored depth is greater than 0 — i.e. nearer than the far
// plane, i.e. where geometry was actually drawn. Background still holds the 0 clear
// value and is rejected by the hardware before the fragment shader runs.
void main() {
    vec2 p = vec2((gl_VertexIndex << 1) & 2, gl_VertexIndex & 2);
    gl_Position = vec4(p * 2.0 - 1.0, 0.0, 1.0);
}
