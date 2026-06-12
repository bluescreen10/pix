const PI: f32 = 3.1415926538;

@group(0) @binding(5) var env_map: texture_2d<f32>;
@group(0) @binding(6) var env_map_sampler: sampler;

@fragment
fn main(@location(0) ray_dir: vec3<f32>) -> @location(0) vec4<f32> {
    let uv = equirect_uv(normalize(ray_dir));
    return textureSample(env_map, env_map_sampler, uv);
    //return vec4f(1,1,1,1);
}

fn equirect_uv(dir: vec3f) -> vec2f {
    let phi   = atan2(dir.z, dir.x);
    let theta = asin(dir.y);
    return vec2f(phi / (2.0 * PI) + 0.5, 0.5 - theta / PI);
}