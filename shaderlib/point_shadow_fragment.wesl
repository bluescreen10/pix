struct PointShadowUniforms {
    view_projection:   mat4x4<f32>,
    light_pos_and_far: vec4<f32>,
}

@group(0) @binding(0) var<uniform> shadow: PointShadowUniforms;

@fragment
fn main(@location(0) world_pos: vec3<f32>) -> @builtin(frag_depth) f32 {
    let light_pos = shadow.light_pos_and_far.xyz;
    let far       = shadow.light_pos_and_far.w;
    return length(world_pos - light_pos) / far;
}
