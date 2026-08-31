// Shared bindless/root definitions, #included by shaders (GL_GOOGLE_include_directive).
#extension GL_EXT_buffer_reference : require
#extension GL_EXT_buffer_reference2 : require
#extension GL_EXT_scalar_block_layout : require
#extension GL_EXT_shader_explicit_arithmetic_types_int64 : require

struct Vertex {
    vec2 pos;
    vec3 color;
};

// A pointer to the vertex array (buffer_device_address).
layout(buffer_reference, scalar) readonly buffer VertexBuf {
    Vertex verts[];
};

// The single root struct every draw points at (address in the push constant).
layout(buffer_reference, scalar) readonly buffer Root {
    vec4 tint;
    VertexBuf vertices;
};

layout(push_constant) uniform Push {
    Root root;
} pc;
