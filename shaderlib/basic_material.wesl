struct FragmentInput {
    @if(USE_UV)
    @location(1) v_uv: vec2<f32>,
}

@group(1) @binding(0) var<uniform> color: vec4<f32>;
@if(USE_MAP) @group(1) @binding(1) var color_map: texture_2d<f32>;
@if(USE_MAP) @group(1) @binding(2) var color_map_sampler: sampler;

@fragment
fn main(in: FragmentInput) -> @location(0) vec4<f32> {
    @if USE_MAP && USE_UV {
        return textureSample(color_map, color_map_sampler, in.v_uv) * color;
    } @else {
        return color;
    }
}
