#version 460
#extension GL_EXT_buffer_reference : require
#extension GL_EXT_buffer_reference2 : require
#extension GL_EXT_scalar_block_layout : require
#extension GL_EXT_shader_explicit_arithmetic_types_int64 : require

// Textured quad: pulls clip-space pos + uv per vertex, forwards uv to the frag
// stage which samples a bindless-heap texture. Exercises the descriptor heap.
struct Vtx { vec2 pos; vec2 uv; };
layout(buffer_reference, scalar) readonly buffer VertexBuf { Vtx v[]; };

layout(buffer_reference, scalar) readonly buffer Root {
    VertexBuf verts;
    uint tex;  // bindless sampled-image index
    uint samp; // bindless sampler index
};
layout(push_constant) uniform PC { Root root; } pc;

layout(location = 0) out vec2 vUV;

void main() {
    Vtx v = pc.root.verts.v[gl_VertexIndex];
    vUV = v.uv;
    gl_Position = vec4(v.pos, 0.0, 1.0);
}
