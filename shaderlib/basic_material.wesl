struct Camera {
    view_projection: mat4x4<f32>,
    position:        vec4<f32>,
}

@group(0) @binding(0) var<uniform>       camera:     Camera;

struct FragmentInput {
    @if(USE_UV)
    @location(1) v_uv: vec2<f32>,
}

@fragment
fn main(@builtin(position) pos: vec4<f32>) -> @location(0) vec4<f32> {
    @if USE_MAP && USE_UV {
        return textureSample(color_map, color_map_sampler, in.v_uv) * color;
    } @else {
        return color;
    }
}
