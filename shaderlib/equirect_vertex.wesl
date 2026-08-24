import vertex::camera;

struct VertexOutput {
    @builtin(position) clip_position: vec4<f32>,
    @location(0)       ray_dir:       vec3<f32>,
}

@vertex
fn main(@builtin(vertex_index) vi: u32) -> VertexOutput {
    // Fullscreen triangle (covers the viewport with 3 vertices).
    var corners = array<vec2<f32>, 3>(
        vec2<f32>(-1.0, -1.0),
        vec2<f32>(-1.0,  3.0),
        vec2<f32>( 3.0, -1.0),
    );
    let clip = vec4<f32>(corners[vi], 0.999, 1.0);
    let dir  = camera.sky_inv_view_proj * clip;

    var out: VertexOutput;
    // z = w forces NDC depth to 1.0 (far plane) after perspective divide,
    // so DepthFuncLessEqual only passes where no geometry was drawn.
    out.clip_position = vec4<f32>(clip.xy, clip.w, clip.w);
    out.ray_dir = dir.xyz;
    return out;
}
