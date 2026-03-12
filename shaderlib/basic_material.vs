#version 450

layout (location = 0) in vec3 position;

#ifdef USE_UV
layout (location = 1) in vec2 uv;
layout (location = 0) out vec2 vUv;
#endif

struct Camera {
    mat4 view_projection;
};

layout(set = 0, binding = 0) uniform Camera camera;

void main() {
    gl_Position = camera.view_projection * vec4(position, 1.0);
    #ifdef USE_UV
    vUv = uv;
    #endif
}

