#version 450

layout (location = 0) in vec3 position;

struct Camera {
    mat4 view_projection;
};

layout(binding = 0) uniform Camera camera;

void main() {
    gl_Position = camera.view_projection * vec4(position, 1.0);
}

