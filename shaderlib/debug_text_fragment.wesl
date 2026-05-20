@group(0) @binding(1) var font_atlas:   texture_2d<f32>;
@group(0) @binding(2) var font_sampler: sampler;

@fragment
fn main(
    @location(0) uv:    vec2<f32>,
    @location(1) color: vec4<f32>,
) -> @location(0) vec4<f32> {
    let alpha = textureSample(font_atlas, font_sampler, uv).r;
    if alpha < 0.5 {
        discard;
    }
    return vec4<f32>(color.rgb, color.a);
}
