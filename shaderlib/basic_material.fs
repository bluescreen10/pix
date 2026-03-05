#version 450

layout(location = 0) in vec2 vUv;

struct Material {
    vec4 color;
    uint hasColorMap;
};

layout(binding = 0, set = 1) uniform Material material;
layout(binding = 1, set = 1) uniform texture2D colorMap;
layout(binding = 2, set = 1) uniform sampler colorMapSampler;

layout(location = 0) out vec4 FragColor;

void main() {
    if (material.hasColorMap != 0u) {
        FragColor = texture(sampler2D(colorMap, colorMapSampler), vUv) * vec4(material.color, 1.0);
        return;
    }
    FragColor = material.color;
}
