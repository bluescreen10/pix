struct PointShadowUniforms {
    view_projection:   mat4x4<f32>,
    light_pos_and_far: vec4<f32>,
}

@group(0) @binding(0) var<uniform>       shadow:     PointShadowUniforms;
@group(1) @binding(0) var<storage, read> models:     array<mat4x4<f32>>;
@group(1) @binding(1) var<storage, read> inv_models: array<mat4x4<f32>>;

struct VertexOutput {
    @builtin(position) clip_position: vec4<f32>,
    @location(0)       world_pos:     vec3<f32>,
}

@vertex
fn main(@location(0) position: vec3<f32>, @builtin(instance_index) ii: u32) -> VertexOutput {
    let world_pos = (models[ii] * vec4<f32>(position, 1.0)).xyz;
    var out: VertexOutput;
    out.clip_position = shadow.view_projection * vec4<f32>(world_pos, 1.0);
    out.world_pos = world_pos;
    return out;
}
