#version 460
#extension GL_EXT_buffer_reference : require
#extension GL_EXT_buffer_reference2 : require
#extension GL_EXT_scalar_block_layout : require
#extension GL_EXT_shader_explicit_arithmetic_types_int64 : require

// Screen-space solid quads for the debug text overlay: one instance per lit font
// pixel. rect = (x, y top-left in pixels, w, h in pixels); color is straight RGBA.
struct Quad { vec4 rect; vec4 color; };
layout(buffer_reference, scalar) readonly buffer QuadBuf { Quad v[]; };

layout(buffer_reference, scalar) readonly buffer Root {
    vec2 viewport; // framebuffer size in pixels
    QuadBuf quads;
};
layout(push_constant) uniform PC { Root root; } pc;

layout(location = 0) out vec4 vColor;

// Two triangles (TL, TR, BR / TL, BR, BL) as a unit-quad corner table.
const vec2 CORNERS[6] = vec2[](
    vec2(0, 0), vec2(1, 0), vec2(1, 1),
    vec2(0, 0), vec2(1, 1), vec2(0, 1));

void main() {
    Quad q = pc.root.quads.v[gl_InstanceIndex];
    vec2 px = q.rect.xy + CORNERS[gl_VertexIndex] * q.rect.zw;
    // Vulkan NDC is Y-down, so top-left pixel (0,0) maps directly to (-1,-1).
    vec2 ndc = px / pc.root.viewport * 2.0 - 1.0;
    gl_Position = vec4(ndc, 0.0, 1.0);
    vColor = q.color;
}
