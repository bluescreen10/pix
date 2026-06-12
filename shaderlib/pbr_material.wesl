import vertex::camera;

const PI: f32 = 3.14159265359;
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
    color: vec4<f32>,
    position: vec4<f32>,
    far: f32,
    casts_shadow: u32,
    bias: f32,
    _pad: f32,
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

struct Material {
    color: vec4<f32>,
    metallic: f32,
     _pad0:     array<f32, 3>,
    roughness: f32,
     _pad1:     array<f32, 3>,
    ao: f32,
}

@group(0) @binding(1) var<uniform> lights: Lights;
@group(0) @binding(2) var shadow_maps: texture_depth_2d_array;
@group(0) @binding(3) var shadow_sampler: sampler_comparison;
@group(0) @binding(4) var point_shadow_maps: texture_depth_cube_array;
@group(0) @binding(5) var env_map: texture_2d<f32>;
@group(0) @binding(6) var env_map_sampler: sampler;

struct FragmentInput {
    @location(0) v_world_pos: vec3<f32>,
    @if(USE_UV) @location(1) v_uv: vec2<f32>,
    @if(USE_NORMAL) @location(2) v_normal: vec3<f32>,
}

@group(1) @binding(0) var<uniform> material: Material;

@if(USE_MAP) @group(1) @binding(1)
var color_map: texture_2d<f32>;

@if(USE_MAP) @group(1) @binding(2)
var color_map_sampler: sampler;

// GGX Normal Distribution Function
fn ndf_ggx(N: vec3<f32>, H: vec3<f32>, roughness: f32) -> f32 {
    let a = roughness * roughness;
    let a2 = a * a;
    let d = max(dot(N, H), 0.0);
    let denom = d * d * (a2 - 1.0) + 1.0;
    return a2 / (PI * denom * denom);
}

// Schlick-GGX Geometry Function
fn geo_schlick(NdotX: f32, roughness: f32) -> f32 {
    let r = roughness + 1.0;
    let k = r * r / 8.0;
    return NdotX / (NdotX * (1.0 - k) + k);
}

// Smith's combined geometry term
fn geo_smith(N: vec3<f32>, V: vec3<f32>, L: vec3<f32>, roughness: f32) -> f32 {
    return geo_schlick(max(dot(N, V), 0.0), roughness)
         * geo_schlick(max(dot(N, L), 0.0), roughness);
}

// Fresnel-Schlick approximation
fn fresnel_schlick(cos_theta: f32, F0: vec3<f32>) -> vec3<f32> {
    return F0 + (1.0 - F0) * pow(clamp(1.0 - cos_theta, 0.0, 1.0), 5.0);
}

// Cook-Torrance BRDF contribution for one light.
// Returns outgoing radiance contribution (already multiplied by NdotL).
fn cook_torrance(N: vec3<f32>, V: vec3<f32>, L: vec3<f32>,
    albedo: vec3<f32>, metallic: f32, roughness: f32) -> vec3<f32> {
    let NdotL = max(dot(N, L), 0.0);
    if NdotL <= 0.0 { return vec3<f32>(0.0); }

    let H = normalize(V + L);
    let F0 = mix(vec3<f32>(0.04), albedo, metallic);

    let D = ndf_ggx(N, H, roughness);
    let G = geo_smith(N, V, L, roughness);
    let F = fresnel_schlick(max(dot(H, V), 0.0), F0);

    let spec = D * G * F / (4.0 * max(dot(N, V), 0.0) * NdotL + 0.0001);
    let kD = (vec3<f32>(1.0) - F) * (1.0 - metallic);

    return (kD * albedo / PI + spec) * NdotL;
}

@fragment
fn main(in: FragmentInput) -> @location(0) vec4<f32> {
    var base_color = material.color;

    @if USE_MAP && USE_UV {
        base_color *= textureSample(color_map, color_map_sampler, in.v_uv);
    }

    let N = normalize(in.v_normal);
    let V = normalize(camera.position.xyz - in.v_world_pos);

    var Lo = vec3<f32>(0.0);

    for (var i = 0u; i < lights.directional_lights_count; i++) {
        let light = lights.directional_lights[i];
        let L = normalize(-light.direction.xyz);
        let radiance = light.color.rgb * light.color.w;

        var shadow = 1.0;

        if light.casts_shadow != 0u {
            let biased_world_pos = in.v_world_pos + N * light.bias;
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

        Lo += shadow * cook_torrance(N, V, L, base_color.rgb, material.metallic, material.roughness) * radiance;
    }

    for (var i = 0u; i < lights.spot_lights_count; i++) {
        let light = lights.spot_lights[i];
        let L = normalize(light.position.xyz - in.v_world_pos);
        let spot_dir = normalize(-light.direction.xyz);
        let cos_angle = dot(L, spot_dir);
        let spot_factor = smoothstep(light.outer_cosine, light.inner_cosine, cos_angle);
        let radiance = light.color.rgb * light.color.w * spot_factor;

        var shadow = 1.0;

        if light.casts_shadow != 0u {
            let biased_world_pos = in.v_world_pos + N * light.bias;
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

        Lo += shadow * cook_torrance(N, V, L, base_color.rgb, material.metallic, material.roughness) * radiance;
    }

    for (var i = 0u; i < lights.point_lights_count; i++) {
        let light = lights.point_lights[i];
        let to_frag = in.v_world_pos - light.position.xyz;
        let dist = length(to_frag);
        let L = normalize(-to_frag);
        let attenuation = 1.0 / (dist * dist);
        let radiance = light.color.rgb * light.color.w * attenuation;
        var shadow = 1.0;


        if light.casts_shadow != 0u {
            let biased_world_pos = in.v_world_pos + N * light.bias;
            let biased_dist = length(biased_world_pos - light.position.xyz);
            let ref_depth = biased_dist / light.far;
            shadow = textureSampleCompare(point_shadow_maps, shadow_sampler, to_frag, i32(i), ref_depth);
        }

        //Lo += cook_torrance(N, V, L, base_color.rgb, material.metallic, material.roughness);
        Lo += shadow * cook_torrance(N, V, L, base_color.rgb, material.metallic, material.roughness) * radiance;
        //Lo += NdotL * radiance;
        //Lo += cook_torrance(N, V, L, vec3<f32>(1.0), 0.0, 0.5);
    }

    let ambient = lights.ambient_light.color.rgb * lights.ambient_light.intensity * base_color.rgb * material.ao;
    let color = ambient + Lo;

    return vec4<f32>(color, base_color.a);
    //return vec4<f32>(material.roughness, material.metallic, material.ao, 1.0);
    //return vec4<f32>(Lo, 1.0);
    
}
