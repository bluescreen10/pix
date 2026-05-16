struct PointShadowUniforms {
    view_projection:   mat4x4<f32>,
    light_pos_and_far: vec4<f32>,
}

struct Object {
    model:     mat4x4<f32>,
    inv_model: mat4x4<f32>,
}

@group(0) @binding(0) var<uniform>       shadow:  PointShadowUniforms;
@group(1) @binding(0) var<storage, read> objects: array<Object>;

struct VertexOutput {
    @builtin(position) clip_position: vec4<f32>,
    @location(0)       world_pos:     vec3<f32>,
}

@vertex
fn main(@location(0) position: vec3<f32>, @builtin(instance_index) ii: u32) -> VertexOutput {
    let world_pos = (objects[ii].model * vec4<f32>(position, 1.0)).xyz;
    var out: VertexOutput;
    out.clip_position = shadow.view_projection * vec4<f32>(world_pos, 1.0);
    out.world_pos = world_pos;
    return out;
}
