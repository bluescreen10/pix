struct VertexOutput {
    @builtin(position) position: vec4<f32>,
}

struct Camera {
     view_projection: mat4x4<f32>,
//     position: vec4<f32>,
}

struct Material {
    base_color: vec4<f32>,
}

@group(0) @binding(0) var<uniform> camera: Camera;
@group(1) @binding(0) var<uniform> material: Material;

 // @group(1) @binding(0) var texture_sampler: sampler;
// @group(1) @binding(1) var base_color_map: texture_2d<f32>;

@vertex
fn vs_main( @location(0) position: vec3<f32>) -> VertexOutput {
    var out: VertexOutput;
    //out.tex_coord = tex_coord;
    out.position = camera.view_projection * vec4<f32>(position,1.0);
    return out;
}

@fragment
fn fs_main(in: VertexOutput) -> @location(0) vec4<f32> {
    return vec4<f32>(1,0,0,1);
}
