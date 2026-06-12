struct Camera {
    view_projection:     mat4x4<f32>,
    inv_view_projection: mat4x4<f32>,
    sky_inv_view_proj:   mat4x4<f32>,
    position:            vec4<f32>,
}

struct VertexInput {
    @location(0) position:    vec3<f32>,
    @if(USE_UV) @location(1) uv: vec2<f32>,
    @if(USE_NORMAL) @location(2) normal: vec3<f32>,
    @if(USE_SKINNING) @location(5) skin_index:  vec4<u32>,
    @if(USE_SKINNING) @location(6) skin_weight: vec4<f32>,
}

struct VertexOutput {
    @builtin(position) clip_position: vec4<f32>,
    @location(0) v_world_pos: vec3<f32>,
    @if(USE_UV) @location(1) v_uv: vec2<f32>,
    @if(USE_NORMAL) @location(2) v_normal: vec3<f32>,
}

@group(0) @binding(0) var<uniform>       camera:     Camera;
@group(2) @binding(0) var<storage, read> models:     array<mat4x4<f32>>;
@group(2) @binding(1) var<storage, read> inv_models: array<mat4x4<f32>>;
@if(USE_SKINNING) @group(3) @binding(0) var<storage, read> bone_matrices: array<mat4x4f>;

@vertex
fn main(in: VertexInput, @builtin(instance_index) instance_index: u32) -> VertexOutput {
    var out: VertexOutput;

    let model     = models[instance_index];
    let inv_model = inv_models[instance_index];

    // Skinning: blend bone matrices by weights (identity when USE_SKINNING is off).
    var skin_mat = mat4x4f(
        vec4f(1.0, 0.0, 0.0, 0.0),
        vec4f(0.0, 1.0, 0.0, 0.0),
        vec4f(0.0, 0.0, 1.0, 0.0),
        vec4f(0.0, 0.0, 0.0, 1.0),
    );
    @if USE_SKINNING {
        skin_mat =
            in.skin_weight.x * bone_matrices[in.skin_index.x] +
            in.skin_weight.y * bone_matrices[in.skin_index.y] +
            in.skin_weight.z * bone_matrices[in.skin_index.z] +
            in.skin_weight.w * bone_matrices[in.skin_index.w];
    }

    let world_pos = model * skin_mat * vec4f(in.position, 1.0);
    out.v_world_pos   = world_pos.xyz;
    out.clip_position = camera.view_projection * world_pos;

    @if USE_UV {
        out.v_uv = in.uv;
    }

    @if USE_NORMAL {
        let skin3 = mat3x3f(skin_mat[0].xyz, skin_mat[1].xyz, skin_mat[2].xyz);
        let normal_matrix = transpose(
            mat3x3f(inv_model[0].xyz, inv_model[1].xyz, inv_model[2].xyz)
        );
        out.v_normal = normalize(normal_matrix * skin3 * in.normal);
    }

    return out;
}
