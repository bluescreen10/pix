// environment.glsl — how an environment image maps onto directions, shared by the lit
// shaders, the passes that derive an environment's light, and its background.
#ifndef PIX_ENVIRONMENT_GLSL
#define PIX_ENVIRONMENT_GLSL

const float ENV_PI = 3.14159265358979;

// ENV_BRDF_SIZE is the side of the BRDF table (see env_brdf.comp.glsl and
// pix.environmentBRDFSize).
const float ENV_BRDF_SIZE = 64.0;

// equirectUV is where a direction lands in an equirectangular environment image: u goes
// once around the vertical, from -x through -z, +x and +z; v from straight up (0) to
// straight down (1). equirectDirection is the way back.
vec2 equirectUV(vec3 dir) {
    return vec2(atan(dir.z, dir.x) / (2.0 * ENV_PI) + 0.5, acos(clamp(dir.y, -1.0, 1.0)) / ENV_PI);
}

vec3 equirectDirection(vec2 uv) {
    float phi = (uv.x - 0.5) * 2.0 * ENV_PI;
    float theta = uv.y * ENV_PI;
    return vec3(sin(theta) * cos(phi), cos(theta), sin(theta) * sin(phi));
}

// cubeDirection is the direction through texel uv, in [0,1] across the face, of cube
// face face (+X, -X, +Y, -Y, +Z, -Z): the inverse of the hardware's cube lookup, so
// that what is written there is what a lookup along the direction reads.
vec3 cubeDirection(uint face, vec2 uv) {
    vec2 st = uv * 2.0 - 1.0;
    vec3 dir;
    switch (face) {
    case 0u: dir = vec3(1.0, -st.y, -st.x); break;
    case 1u: dir = vec3(-1.0, -st.y, st.x); break;
    case 2u: dir = vec3(st.x, 1.0, st.y); break;
    case 3u: dir = vec3(st.x, -1.0, -st.y); break;
    case 4u: dir = vec3(st.x, -st.y, 1.0); break;
    default: dir = vec3(-st.x, -st.y, -1.0); break;
    }
    return normalize(dir);
}

// unrotateEnvironment turns a world direction into the frame of an environment turned
// about the vertical by the angle whose cosine and sine are rotation.
vec3 unrotateEnvironment(vec3 dir, vec2 rotation) {
    float c = rotation.x, s = rotation.y;
    return vec3(c * dir.x + s * dir.z, dir.y, -s * dir.x + c * dir.z);
}

// unrotateEnvironment turns a world direction into the frame of an environment turned
// rotation radians about the vertical.
vec3 unrotateEnvironment(vec3 dir, float rotation) {
    return unrotateEnvironment(dir, vec2(cos(rotation), sin(rotation)));
}

#endif // PIX_ENVIRONMENT_GLSL
