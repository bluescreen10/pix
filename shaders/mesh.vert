#version 460
#extension GL_EXT_buffer_reference : require
#extension GL_EXT_buffer_reference2 : require
#extension GL_EXT_scalar_block_layout : require

struct Vertex3D { vec3 pos; vec3 color; };

layout(buffer_reference, scalar) readonly buffer VertexBuf { Vertex3D verts[]; };

layout(buffer_reference, scalar) readonly buffer MeshRoot {
    mat4     mvp;
    VertexBuf vertices;
};

layout(push_constant) uniform MPush { MeshRoot root; } mpc;

layout(location = 0) out vec3 vColor;

void main() {
    Vertex3D v = mpc.root.vertices.verts[gl_VertexIndex];
    gl_Position = mpc.root.mvp * vec4(v.pos, 1.0);
    vColor = v.color;
}
