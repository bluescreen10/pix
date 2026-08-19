struct Drawable {
    bounds:       vec4<f32>,
    instance:     u32,
    transform_id: u32,
    geometry_id:  u32,
    material_id:  u32,
    skeleton_id:  u32,
    owner_node:   u32,
    flags:        u32,
    _pad:         u32,
}

@group(0) @binding(0) var<uniform>       light_vp:     mat4x4<f32>;
@group(1) @binding(0) var<storage, read> models:       array<mat4x4<f32>>;
@group(1) @binding(1) var<storage, read> inv_models:   array<mat4x4<f32>>;
@group(1) @binding(2) var<storage, read> drawables:    array<Drawable>;
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
    let transform_id = drawables[instance_index].transform_id;
    return light_vp * models[transform_id] * skin_mat * vec4f(in.position, 1.0);
}
