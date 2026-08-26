#version 460
#extension GL_EXT_buffer_reference : require
#extension GL_EXT_buffer_reference2 : require
#extension GL_EXT_scalar_block_layout : require

struct Vertex3D { vec3 pos; vec3 color; };
struct Drawable { vec4 bounds; uint transformID; };

layout(buffer_reference, scalar) readonly buffer VertexBuf   { Vertex3D verts[]; };
layout(buffer_reference, scalar) readonly buffer ModelBuf    { mat4 models[]; };
layout(buffer_reference, scalar) readonly buffer DrawableBuf { Drawable items[]; };
layout(buffer_reference, scalar) readonly buffer VisibleBuf  { uint items[]; };

layout(buffer_reference, scalar) readonly buffer DrawRoot {
    mat4        viewProj;
    VertexBuf   verts;
    ModelBuf    models;
    DrawableBuf drawables;
    VisibleBuf  visible;
};

layout(push_constant) uniform P { DrawRoot root; } pc;

layout(location = 0) out vec3 vColor;

void main() {
    uint drawableIdx = pc.root.visible.items[gl_InstanceIndex];
    Drawable d = pc.root.drawables.items[drawableIdx];
    mat4 model = pc.root.models.models[d.transformID];
    Vertex3D v = pc.root.verts.verts[gl_VertexIndex];
    gl_Position = pc.root.viewProj * model * vec4(v.pos, 1.0);
    vColor = v.color;
}
