import vertex::camera;

const SHININESS: f32 = 32.0;
const MAX_DIRECTIONAL_LIGHTS: u32 = 5;
const MAX_POINT_LIGHTS: u32 = 0;
const MAX_SPOT_LIGHTS: u32 = 0;

struct DirectionalLight {
    color: vec4<f32>,
    direction: vec4<f32>,
    light_space_matrix: mat4x4<f32>,
    casts_shadow: u32,
    bias: f32,
}

struct AmbientLight {
    color: vec4<f32>,
    intensity: f32,
}

struct Lights {
    directional_lights: array<DirectionalLight, MAX_DIRECTIONAL_LIGHTS>,
    directional_lights_count: u32,
    ambient_light: AmbientLight,
}

@group(0) @binding(1) var<uniform> lights: Lights;
@group(0) @binding(2) var shadow_maps: texture_depth_2d_array;
@group(0) @binding(3) var shadow_sampler: sampler_comparison;

struct FragmentInput {
    @location(0) v_world_pos: vec3<f32>,
    @if(USE_UV) @location(1) v_uv: vec2<f32>,
    @if(USE_NORMAL) @location(2) v_normal: vec3<f32>,
}

@group(1) @binding(0)
var<uniform> color: vec4<f32>;

@if(USE_MAP) @group(1) @binding(1)
var color_map: texture_2d<f32>;

@if(USE_MAP) @group(1) @binding(2)
var color_map_sampler: sampler;

@fragment
fn main(in: FragmentInput) -> @location(0) vec4<f32> {

    var base_color = color;

    @if USE_MAP && USE_UV {
        base_color = textureSample(color_map, color_map_sampler, in.v_uv) * color;
    }

    let normal = normalize(in.v_normal);
    let view_dir = normalize(camera.position.xyz - in.v_world_pos);

    var lighting = lights.ambient_light.color.rgb * lights.ambient_light.intensity;

    for (var i = 0u; i < lights.directional_lights_count; i++) {
        let light = lights.directional_lights[i];
        let light_dir = normalize(-light.direction.xyz);
        let intensity = light.color.w;
        let diff = max(dot(normal, light_dir), 0.0);

        let half_dir = normalize(light_dir + view_dir);
        let spec = pow(max(dot(normal, half_dir), 0.0), SHININESS);

        var shadow = 1.0;

        if light.casts_shadow != 0u {
            let biased_world_pos = in.v_world_pos + normal * light.bias;
            let light_clip = light.light_space_matrix * vec4<f32>(biased_world_pos, 1.0);
            let ndc = light_clip.xyz / light_clip.w;
            let shadow_uv = ndc.xy * vec2<f32>(0.5, -0.5) + vec2<f32>(0.5);
            let shadow_depth = ndc.z;
            let valid = light_clip.w > 0.0
                && shadow_uv.x >= 0.0 && shadow_uv.x <= 1.0
                && shadow_uv.y >= 0.0 && shadow_uv.y <= 1.0
                && shadow_depth >= 0.0 && shadow_depth <= 1.0;

            let safe_uv = clamp(shadow_uv, vec2<f32>(0.0), vec2<f32>(1.0));
            let sampled = textureSampleCompare(shadow_maps, shadow_sampler, safe_uv, i32(i), shadow_depth);
            shadow = select(1.0, sampled, valid);
        }

        lighting += shadow * (diff + spec) * light.color.rgb * intensity;
    }

    return vec4<f32>(base_color.rgb * lighting, base_color.a);
}