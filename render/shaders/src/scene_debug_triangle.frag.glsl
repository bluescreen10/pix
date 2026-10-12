#version 460

// One flat colour per triangle, wrapping through the palette.
//
// gl_PrimitiveID is a free fragment-stage builtin, so unlike the object view this
// needs nothing forwarded from the vertex stage at all.
#include "scene_debug_palette.glsl"

layout(location = 0) flat in uint vObjectID; // unused; declared to match the vertex stage
layout(location = 0) out vec4 outColor;

void main() {
    outColor = debugIDColor(uint(gl_PrimitiveID));
}
