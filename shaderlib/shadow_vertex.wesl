@group(0) @binding(0) var<uniform>       light_vp: mat4x4<f32>;
@group(1) @binding(0) var<storage, read> models:   array<mat4x4<f32>>;
@group(1) @binding(1) var<storage, read> inv_models: array<mat4x4<f32>>;

@vertex
fn main( @location(0) position: vec3<f32>,
@builtin(instance_index) instance_index: u32,
) -> @builtin(position) vec4<f32> {
    return light_vp * models[instance_index] * vec4<f32>(position, 1.0);
}
