#version 460
#extension GL_EXT_buffer_reference : require
#extension GL_EXT_buffer_reference2 : require
#extension GL_EXT_scalar_block_layout : require
#extension GL_EXT_shader_explicit_arithmetic_types_int64 : require

// Screen-space quads for the debug overlay: one instance per character (or per solid
// fill). rect = (x, y top-left in pixels, w, h in pixels); uv = (u0, v0, u1, v1) into
// the glyph atlas; color is straight RGBA, modulated by the atlas coverage.
//
// Solid fills carry a degenerate uv rect pointing at the atlas's fully-opaque cell, so
// they take the same path as glyphs and the shader needs no branch.
struct Quad { vec4 rect; vec4 uv; vec4 color; };
layout(buffer_reference, scalar) readonly buffer QuadBuf { Quad v[]; };

layout(buffer_reference, scalar) readonly buffer Root {
    vec2 viewport; // framebuffer size in pixels
    uint atlas;    // bindless index of the glyph atlas
    uint samp;     // bindless index of its sampler
    QuadBuf quads;
};
layout(push_constant) uniform PC { Root root; } pc;

layout(location = 0) out vec4 vColor;
layout(location = 1) out vec2 vUV;

// Two triangles (TL, TR, BR / TL, BR, BL) as a unit-quad corner table.
const vec2 CORNERS[6] = vec2[](
    vec2(0, 0), vec2(1, 0), vec2(1, 1),
    vec2(0, 0), vec2(1, 1), vec2(0, 1));

void main() {
    Quad q = pc.root.quads.v[gl_InstanceIndex];
    vec2 corner = CORNERS[gl_VertexIndex];
    vec2 px = q.rect.xy + corner * q.rect.zw;
    // Vulkan NDC is Y-down, so top-left pixel (0,0) maps directly to (-1,-1).
    vec2 ndc = px / pc.root.viewport * 2.0 - 1.0;
    gl_Position = vec4(ndc, 0.0, 1.0);
    vColor = q.color;
    vUV = mix(q.uv.xy, q.uv.zw, corner);
}
