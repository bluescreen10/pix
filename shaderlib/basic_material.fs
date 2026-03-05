#version 450

struct Material {
    vec3 color;
};

layout(set = 1, binding = 0) uniform Material material;

layout(location = 0) out vec4 FragColor;

void main() {
    FragColor = vec4(material.color, 1.0);
}
