import global;

const GLOBAL_SET :i32 = 0;
const INSTANCE_SET :i32 = 2;

struct Camera {
    view_projection: mat4x4<f32>,
    position:        vec4<f32>,
}

struct Object {
    model:     mat4x4<f32>,
    inv_model: mat4x4<f32>,
}

struct VertexInput {
    @location(0)              position: vec3<f32>,
    @if(USE_UV) @location(1) uv:       vec2<f32>,
}

struct VertexOutput {
    @builtin(position)        clip_position: vec4<f32>,
    @if(USE_UV) @location(1) v_uv:          vec2<f32>,
}

@group(global::GLOBAL_SET) @binding(0) var<uniform>       camera:  Camera;
@group(global::INSTANCE_SET) @binding(0) var<storage, read> objects: array<Object>;

@vertex
fn vs_main(
    in: VertexInput,
    @builtin(instance_index) instance_index: u32,
) -> VertexOutput {
    var out: VertexOutput;
    let object = objects[instance_index];
    out.clip_position = camera.view_projection * object.model * vec4<f32>(in.position, 1.0);
    @if(USE_UV) {
        out.v_uv = in.uv;
    }
    return out;
}

struct FragmentInput {
    @if(USE_UV) @location(1) v_uv: vec2<f32>,
}

@group(1) @binding(0) var<uniform>        color:             vec4<f32>;
@if(USE_MAP) @group(1) @binding(1) var    color_map:         texture_2d<f32>;
@if(USE_MAP) @group(1) @binding(2) var    color_map_sampler: sampler;

@fragment
fn fs_main(in: FragmentInput) -> @location(0) vec4<f32> {
    @if(USE_MAP && USE_UV) {
        return textureSample(color_map, color_map_sampler, in.v_uv) * color;
    } @else {
        return color;
    }
}
