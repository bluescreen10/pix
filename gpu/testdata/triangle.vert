#version 460
#extension GL_GOOGLE_include_directive : require
#include "common.glsl"

layout(location = 0) out vec3 vColor;

void main() {
    Vertex v = pc.root.vertices.verts[gl_VertexIndex];
    gl_Position = vec4(v.pos, 0.0, 1.0);
    vColor = v.color * pc.root.tint.rgb;
}
