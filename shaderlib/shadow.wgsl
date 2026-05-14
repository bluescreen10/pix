struct Object {
    model:     mat4x4<f32>,
    inv_model: mat4x4<f32>,
}

@group(0) @binding(0) var<uniform>       light_vp: mat4x4<f32>;
@group(1) @binding(0) var<storage, read> objects:  array<Object>;

@vertex
fn vs_main(
    @location(0) position: vec3<f32>,
    @builtin(instance_index) instance_index: u32,
) -> @builtin(position) vec4<f32> {
    let object = objects[instance_index];
    return light_vp * object.model * vec4<f32>(position, 1.0);
}
