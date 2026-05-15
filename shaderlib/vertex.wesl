struct Camera {
    view_projection: mat4x4<f32>,
    position:        vec4<f32>,
}

struct Object {
    model:     mat4x4<f32>,
    inv_model: mat4x4<f32>,
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

@group(0) @binding(0) var<uniform>       camera:  Camera;
@group(2) @binding(0) var<storage, read> objects: array<Object>;

@vertex 
fn main( in: VertexInput, @builtin(instance_index) instance_index: u32) -> VertexOutput {
    var out: VertexOutput;
    
    let object = objects[instance_index];

    let world_pos = object.model * vec4<f32>(in.position, 1.0);

    out.v_world_pos = world_pos.xyz;
    out.clip_position = camera.view_projection * world_pos;

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
