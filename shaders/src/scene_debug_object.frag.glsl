#version 460

// One flat colour per drawable: every triangle of an object shares it.
//
// vObjectID is the drawable index, forwarded flat from scene_debug_id.vert. No push
// constant — the vertex stage owns the root, and this stage needs nothing from it.
#include "scene_debug_palette.glsl"

layout(location = 0) flat in uint vObjectID;
layout(location = 0) out vec4 outColor;

void main() {
    outColor = debugIDColor(vObjectID);
}
