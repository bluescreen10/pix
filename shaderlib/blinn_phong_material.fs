#version 450

layout (location = 0) in vec3 vWorldPos;


#ifdef USE_UV
layout(location = 1) in vec2 vUv;
#endif

#ifdef USE_NORMAL
layout(location = 2) in vec3 vNormal;
#endif

struct Camera {
    mat4 view_projection;
    vec4 position;
};


struct DirectionalLight {
    vec4 color;
    vec4 direction;
};

struct Lights {
    DirectionalLight directional_lights[MAX_DIRECTIONAL_LIGHTS];
    uint directional_lights_count;
};

layout(set = GLOBAL_SET, binding = 0) uniform Camera camera;
layout(set = GLOBAL_SET, binding = 1) uniform Lights lights;


layout(set = MATERIAL_SET, binding = 0) uniform vec4 color;
layout(set = MATERIAL_SET, binding = 1) uniform texture2D colorMap;
layout(set = MATERIAL_SET, binding = 2) uniform sampler colorMapSampler;

layout(location = 0) out vec4 FragColor;

const float AMBIENT = 0.03;
const float SHININESS = 32.0;

void main() {
     vec4 baseColor = color;

    #if defined(USE_MAP) && defined(USE_UV)
    baseColor = texture(sampler2D(colorMap, colorMapSampler), vUv) * color;
    #endif

    vec3 normal = normalize(vNormal);
    vec3 viewDir = normalize(camera.position.xyz - vWorldPos);
    vec3 lighting = vec3(AMBIENT);

    for (uint i = 0u; i < lights.directional_lights_count; i++) {
        DirectionalLight light = lights.directional_lights[i];
        vec3 lightDir = normalize(-light.direction.xyz);
        float intensity = light.color.w;

        float diff = max(dot(normal, lightDir), 0.0);

        vec3 halfDir = normalize(lightDir + viewDir);
        float spec = pow(max(dot(normal, halfDir), 0.0), SHININESS);

        lighting += (diff + spec) * light.color.rgb * intensity;
    }

    FragColor = vec4(baseColor.rgb * lighting, baseColor.a);
}
