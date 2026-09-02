#version 460

// Depth-only shadow pass: the render target has no color attachment, so the
// fragment stage outputs nothing — depth is written by fixed-function. A fragment
// shader is still bound because the pipeline wrapper requires one.
void main() {
}
