#version 450

layout (location = 0) in vec3 position;
layout (location = 0) out vec3 vWorldPos;

#ifdef USE_UV
layout (location = 1) in vec2 uv;
layout (location = 1) out vec2 vUv;
#endif

#ifdef USE_NORMAL
layout (location = 2) in vec3 normal;
layout(location = 2) out vec3 vNormal;
#endif

struct Camera {
    mat4 view_projection;
    vec4 position;
};

struct Object {
    mat4 model;
    mat4 invModel;
};

layout(set = GLOBAL_SET, binding = 0) uniform Camera camera;

layout(std430, set = OBJECT_SET, binding = 0) readonly buffer Objects {
    Object []objects;
};


void main() {
    Object object = objects[gl_InstanceIndex];
    vec4 worldPos = object.model * vec4(position, 1.0);
    vWorldPos = worldPos.xyz;
    gl_Position = camera.view_projection * worldPos;
    
    #ifdef USE_UV
    vUv = uv;
    #endif

     #ifdef USE_NORMAL
    vec3 n = mat3(transpose(objects[gl_InstanceIndex].invModel)) * normal;
    vNormal = normalize(n);
    #endif
}
