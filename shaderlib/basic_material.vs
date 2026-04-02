#version 450

layout (location = 0) in vec3 position;

#ifdef USE_UV
layout (location = 1) in vec2 uv;
layout (location = 1) out vec2 vUv;
#endif

struct Camera {
    mat4 view_projection;
    vec4 position;
};

layout(set = GLOBAL_SET, binding = 0) uniform Camera camera;

struct Object {
    mat4 model;
    mat4 invModel;
};


layout(std430, set = INSTANCE_SET, binding = 0) readonly buffer Objects {
    Object []objects;
};

void main() {
    Object object  = objects[gl_InstanceIndex];
    gl_Position = camera.view_projection * object.model * vec4(position, 1.0);
    
    #ifdef USE_UV
    vUv = uv;
    #endif
}
