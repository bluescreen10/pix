const SHININESS: f32 = 32.0;
const MAX_DIRECTIONAL_LIGHTS: u32 = 5;
const MAX_POINT_LIGHTS: u32 = 0;
const MAX_SPOT_LIGHTS: u32 = 0;

struct Camera {
    view_projection: mat4x4<f32>,
    position: vec4<f32>,
}

struct Object {
    model: mat4x4<f32>,
    inv_model: mat4x4<f32>,
}

struct DirectionalLight {
    color: vec4<f32>, // rgb + intensity in w
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

struct VertexInput {
    @location(0) position: vec3<f32>,
    @if(USE_UV) @location(1) uv: vec2<f32>,
    @if(USE_NORMAL) @location(2) normal: vec3<f32>,
}

struct VertexOutput {
    @builtin(position) clip_position: vec4<f32>,
    @location(0) v_world_pos: vec3<f32>,
    @if(USE_UV) @location(1) v_uv: vec2<f32>,
    @if(USE_NORMAL) @location(2) v_normal: vec3<f32>,
}

@group(0) @binding(0)
var<uniform> camera: Camera;

@group(0) @binding(1)
var<uniform> lights: Lights;

@group(0) @binding(2)
var shadow_maps: texture_depth_2d_array;

@group(0) @binding(3)
var shadow_sampler: sampler_comparison;

@group(2) @binding(0)
var<storage, read> objects: array<Object>;

@vertex
fn vs_main(
    in: VertexInput,
    @builtin(instance_index) instance_index: u32,
) -> VertexOutput {

    var out: VertexOutput;

    let object = objects[instance_index];

    let world_pos = object.model *
        vec4<f32>(in.position, 1.0);

    out.v_world_pos = world_pos.xyz;

    out.clip_position = camera.view_projection *
        world_pos;

    @if USE_UV {
        out.v_uv = in.uv;
    }

    @if USE_NORMAL {

        // inverse transpose normal matrix
        let normal_matrix = transpose(
            mat3x3<f32>(
                object.inv_model[0].xyz,
                object.inv_model[1].xyz,
                object.inv_model[2].xyz,
            )
        );

        out.v_normal = normalize(normal_matrix * in.normal);
    }

    return out;
}

struct FragmentInput {
    @location(0) v_world_pos: vec3<f32>,
    @if(USE_UV) @location(1) v_uv: vec2<f32>,
    @if(USE_NORMAL) @location(2) v_normal: vec3<f32>,
}

@group(1) @binding(0)
var<uniform> color: vec4<f32>;

@if(USE_MAP)
@group(1) @binding(1)
var color_map: texture_2d<f32>;

@if(USE_MAP)
@group(1) @binding(2)
var color_map_sampler: sampler;

@fragment
fn fs_main(in: FragmentInput) -> @location(0) vec4<f32> {

    var base_color = color;

    @if USE_MAP && USE_UV {
        base_color = textureSample(
            color_map,
            color_map_sampler,
            in.v_uv
        ) * color;
    }

    let normal = normalize(in.v_normal);

    let view_dir = normalize(camera.position.xyz - in.v_world_pos);

    var lighting = lights.ambient_light.color.rgb *
        lights.ambient_light.intensity;

    for (var i = 0u; i < lights.directional_lights_count; i++) {

        let light = lights.directional_lights[i];

        let light_dir = normalize(-light.direction.xyz);

        let intensity = light.color.w;

        //
        // diffuse
        //

        let diff = max(dot(normal, light_dir), 0.0);

        //
        // specular
        //

        let half_dir = normalize(light_dir + view_dir);

        let spec = pow(
            max(dot(normal, half_dir), 0.0),
            SHININESS
        );

        //
        // shadows
        //

        var shadow = 1.0;

        if light.casts_shadow != 0u {

            let light_clip = light.light_space_matrix *
        vec4<f32>(in.v_world_pos, 1.0);

            let ndc = light_clip.xyz / light_clip.w;

            let shadow_uv = ndc.xy *
        vec2<f32>(0.5, -0.5) +
        vec2<f32>(0.5);

            let shadow_depth = ndc.z;

            let valid = light_clip.w > 0.0 &&
        shadow_uv.x >= 0.0 &&
        shadow_uv.x <= 1.0 &&
        shadow_uv.y >= 0.0 &&
        shadow_uv.y <= 1.0 &&
        shadow_depth >= 0.0 &&
        shadow_depth <= 1.0;

            //
            // Clamp BEFORE sampling
            // so sampling is always legal.
            //

            let safe_uv = clamp(
                shadow_uv,
                vec2<f32>(0.0),
                vec2<f32>(1.0)
            );

            let bias = max(
                light.bias * (1.0 - dot(normal, light_dir)),
                light.bias * 0.1
            );

            //
            // MUST happen in uniform control flow
            //

            let sampled = textureSampleCompare(
                shadow_maps,
                shadow_sampler,
                safe_uv,
                i32(i),
                shadow_depth - bias
            );

            //
            // Mask invalid samples afterward
            //

            shadow = select(1.0, sampled, valid);
        }

        //
        // final directional contribution
        //
        lighting += shadow *
            (diff + spec) *
            light.color.rgb *
            intensity;
    }

    //
    // final shaded color
    //

    return vec4<f32>(
        base_color.rgb * lighting,
        base_color.a
    );
}
