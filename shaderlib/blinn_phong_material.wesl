import vertex::camera;

const SHININESS: f32 = 32.0;
const MAX_DIRECTIONAL_LIGHTS: u32 = 5;
const MAX_SPOT_LIGHTS: u32 = 5;
const MAX_POINT_LIGHTS: u32 = 5;

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

struct SpotLight {
    color: vec4<f32>,
    position: vec4<f32>,
    direction: vec4<f32>,
    light_space_matrix: mat4x4<f32>,
    inner_cosine: f32,
    outer_cosine: f32,
    casts_shadow: u32,
    bias: f32,
}

struct PointLight {
    color:        vec4<f32>,
    position:     vec4<f32>,
    far:          f32,
    casts_shadow: u32,
    bias:         f32,
    _pad:         f32,
}

struct Lights {
    directional_lights: array<DirectionalLight, MAX_DIRECTIONAL_LIGHTS>,
    directional_lights_count: u32,
    ambient_light: AmbientLight,
    spot_lights: array<SpotLight, MAX_SPOT_LIGHTS>,
    spot_lights_count: u32,
    point_lights: array<PointLight, MAX_POINT_LIGHTS>,
    point_lights_count: u32,
}

@group(0) @binding(1) var<uniform> lights: Lights;
@group(0) @binding(2) var shadow_maps: texture_depth_2d_array;
@group(0) @binding(3) var shadow_sampler: sampler_comparison;
@group(0) @binding(4) var point_shadow_maps: texture_depth_cube_array;

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

    let view_dir = normalize(camera.position.xyz - in.v_world_pos);

    var lighting = lights.ambient_light.color.rgb * lights.ambient_light.intensity;

    for (var i = 0u; i < lights.directional_lights_count; i++) {
        let light = lights.directional_lights[i];
        let light_dir = normalize(-light.direction.xyz);
        let intensity = light.color.w;
        let diff = max(dot(in.v_normal, light_dir), 0.0);

        let half_dir = normalize(light_dir + view_dir);
        let spec = pow(max(dot(in.v_normal, half_dir), 0.0), SHININESS);

        var shadow = 1.0;

        if light.casts_shadow != 0u {
            let biased_world_pos = in.v_world_pos + in.v_normal * light.bias;
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

    for (var i = 0u; i < lights.spot_lights_count; i++) {
        let light = lights.spot_lights[i];
        let to_light = light.position.xyz - in.v_world_pos;
        let light_dir = normalize(to_light);
        let spot_dir = normalize(-light.direction.xyz);
        let cos_angle = dot(light_dir, spot_dir);
        let spot_factor = smoothstep(light.outer_cosine, light.inner_cosine, cos_angle);

        let intensity = light.color.w;
        let diff = max(dot(in.v_normal, light_dir), 0.0);
        let half_dir = normalize(light_dir + view_dir);
        let spec = pow(max(dot(in.v_normal, half_dir), 0.0), SHININESS);

        var shadow = 1.0;

        if light.casts_shadow != 0u {
            let biased_world_pos = in.v_world_pos + in.v_normal * light.bias;
            let light_clip = light.light_space_matrix * vec4<f32>(biased_world_pos, 1.0);
            let ndc = light_clip.xyz / light_clip.w;
            let shadow_uv = ndc.xy * vec2<f32>(0.5, -0.5) + vec2<f32>(0.5, 0.5);
            let shadow_depth = ndc.z;
            let valid = light_clip.w > 0.0
                && shadow_uv.x >= 0.0 && shadow_uv.x <= 1.0
                && shadow_uv.y >= 0.0 && shadow_uv.y <= 1.0
                && shadow_depth >= 0.0 && shadow_depth <= 1.0;

            let safe_uv = clamp(shadow_uv, vec2<f32>(0.0), vec2<f32>(1.0));
            let layer = i32(MAX_DIRECTIONAL_LIGHTS) + i32(i);
            let sampled = textureSampleCompare(shadow_maps, shadow_sampler, safe_uv, layer, shadow_depth);
            shadow = select(1.0, sampled, valid);
        }

        lighting += shadow * spot_factor * (diff + spec) * light.color.rgb * intensity;
    }

    for (var i = 0u; i < lights.point_lights_count; i++) {
        let light = lights.point_lights[i];
        let to_frag = in.v_world_pos - light.position.xyz;
        let dist = length(to_frag);
        let light_dir = normalize(-to_frag);
        let intensity = light.color.w;
        let diff = max(dot(in.v_normal, light_dir), 0.0);
        let half_dir = normalize(light_dir + view_dir);
        let spec = pow(max(dot(in.v_normal, half_dir), 0.0), SHININESS);

        var shadow = 1.0;

        if light.casts_shadow != 0u {
            let biased_world_pos = in.v_world_pos + in.v_normal * light.bias;
            let biased_dist = length(biased_world_pos - light.position.xyz);
            let ref_depth = biased_dist / light.far;
            shadow = textureSampleCompare(point_shadow_maps, shadow_sampler, to_frag, i32(i), ref_depth);
        }

        lighting += shadow * (diff + spec) * light.color.rgb * intensity;
    }

    return vec4<f32>(base_color.rgb * lighting, base_color.a);
}
