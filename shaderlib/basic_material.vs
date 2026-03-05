#version 450

layout (location = 0) in vec3 position;
layout (location = 1) in vec2 uv;

struct Camera {
    mat4 view_projection;
};

layout(set = 0, binding = 0) uniform Camera camera;

layout (location = 0) out vec2 vUv;

void main() {
    gl_Position = camera.view_projection * vec4(position, 1.0);
    vUv = uv;
}

