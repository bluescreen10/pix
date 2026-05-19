@group(0) @binding(0) var<uniform>       light_vp:     mat4x4<f32>;
@group(1) @binding(0) var<storage, read> models:       array<mat4x4<f32>>;
@group(1) @binding(1) var<storage, read> inv_models:   array<mat4x4<f32>>;
@if(USE_SKINNING) @group(2) @binding(0) var<storage, read> bone_matrices: array<mat4x4f>;

struct ShadowVertexInput {
    @location(0) position: vec3<f32>,
    @if(USE_SKINNING) @location(5) skin_index:  vec4<u32>,
    @if(USE_SKINNING) @location(6) skin_weight: vec4<f32>,
}

@vertex
fn main(in: ShadowVertexInput, @builtin(instance_index) instance_index: u32) -> @builtin(position) vec4<f32> {
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
    return light_vp * models[instance_index] * skin_mat * vec4f(in.position, 1.0);
}
