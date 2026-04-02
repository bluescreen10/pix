#version 450

#ifdef USE_UV
layout(location = 1) in vec2 vUv;
#endif

// struct Material {
//     vec4 color;
// };

//layout(binding = 0, set = 1) uniform Material material;
layout(set = MATERIAL_SET, binding = 0) uniform vec4 color;
layout(set = MATERIAL_SET, binding = 1) uniform texture2D colorMap;
layout(set = MATERIAL_SET, binding = 2) uniform sampler colorMapSampler;

layout(location = 0) out vec4 FragColor;

void main() {
    #if defined(USE_MAP) && defined(USE_UV)
        FragColor = texture(sampler2D(colorMap, colorMapSampler), vUv) * vec4(color, 1.0);
    #else
        FragColor = color;
    #endif
}
