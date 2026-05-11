const AMBIENT:                f32 = 0.13;
const SHININESS:              f32 = 32.0;
const MAX_DIRECTIONAL_LIGHTS: u32 = 5;

struct Camera {
    view_projection: mat4x4<f32>,
    position:        vec4<f32>,
}

struct Object {
    model:     mat4x4<f32>,
    inv_model: mat4x4<f32>,
}

struct DirectionalLight {
    color:     vec4<f32>,
    direction: vec4<f32>,
}

struct AmbientLight {
    color: vec4<f32>,
    intensity: f32,
}

struct Lights {
    directional_lights:       array<DirectionalLight, MAX_DIRECTIONAL_LIGHTS>,
    directional_lights_count: u32,

    ambient_light: AmbientLight,
}

struct VertexInput {
    @location(0)                  position: vec3<f32>,
    @if(USE_UV)     @location(1)  uv:       vec2<f32>,
    @if(USE_NORMAL) @location(2)  normal:   vec3<f32>,
}

struct VertexOutput {
    @builtin(position)            clip_position: vec4<f32>,
    @location(0)                  v_world_pos:   vec3<f32>,
    @if(USE_UV)     @location(1)  v_uv:          vec2<f32>,
    @if(USE_NORMAL) @location(2)  v_normal:      vec3<f32>,
}

@group(0) @binding(0) var<uniform>       camera:  Camera;
@group(0) @binding(1) var<uniform>       lights:  Lights;
@group(2) @binding(0) var<storage, read> objects: array<Object>;

@vertex
fn vs_main(
    in: VertexInput,
    @builtin(instance_index) instance_index: u32,
) -> VertexOutput {
    var out: VertexOutput;
    let object    = objects[instance_index];
    let world_pos = object.model * vec4<f32>(in.position, 1.0);
    out.v_world_pos   = world_pos.xyz;
    out.clip_position = camera.view_projection * world_pos;
    @if(USE_UV) {
        out.v_uv = in.uv;
    }
    @if(USE_NORMAL) {
        let normal_matrix = mat3x3<f32>(
            object.inv_model[0].xyz,
            object.inv_model[1].xyz,
            object.inv_model[2].xyz,
        );
        out.v_normal = normalize(transpose(normal_matrix) * in.normal);
    }
    return out;
}

struct FragmentInput {
    @location(0)                  v_world_pos: vec3<f32>,
    @if(USE_UV)     @location(1)  v_uv:        vec2<f32>,
    @if(USE_NORMAL) @location(2)  v_normal:    vec3<f32>,
}

@group(1) @binding(0) var<uniform>      color:             vec4<f32>;
@if(USE_MAP) @group(1) @binding(1) var  color_map:         texture_2d<f32>;
@if(USE_MAP) @group(1) @binding(2) var  color_map_sampler: sampler;

@fragment
fn fs_main(in: FragmentInput) -> @location(0) vec4<f32> {
    var base_color = color;
    @if(USE_MAP && USE_UV) {
        base_color = textureSample(color_map, color_map_sampler, in.v_uv) * color;
    }

    let normal   = normalize(in.v_normal);
    let view_dir = normalize(camera.position.xyz - in.v_world_pos);
    var lighting = vec3<f32>(lights.ambient_light.intensity);

    for (var i = 0u; i < lights.directional_lights_count; i++) {
        let light     = lights.directional_lights[i];
        let light_dir = normalize(-light.direction.xyz);
        let intensity = light.color.w;

        let diff     = max(dot(normal, light_dir), 0.0);
        let half_dir = normalize(light_dir + view_dir);
        let spec     = pow(max(dot(normal, half_dir), 0.0), SHININESS);

        lighting += (diff + spec) * light.color.rgb * intensity;
    }

    return vec4<f32>(base_color.rgb * lighting, base_color.a);
}
