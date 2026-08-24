// Point-light cube-face shadow vertex shader — pulls position only, resolving the
// drawable through the cull view's compacted visible buffer. No vertex buffers,
// no skinning (skinned meshes cast their bind pose, matching the main pass). It
// forwards world position so the fragment can write linear (dist/far) depth.

struct PointShadowUniforms {
    view_projection:   mat4x4<f32>,
    light_pos_and_far: vec4<f32>,
}

struct Drawable {
    bounds:       vec4<f32>,
    instance:     u32,
    transform_id: u32,
    geometry_id:  u32,
    material_id:  u32,
    skeleton_id:  u32,
    owner_node:   u32,
    flags:        u32,
    batch_id:     u32,
}

struct GeometryDesc {
    position_base:  u32,
    attribute_base: u32,
    index_base:     u32,
    index_count:    u32,
    flags:          u32,
    _pad:           u32,
}

@group(0) @binding(0) var<uniform>       shadow:    PointShadowUniforms;

@group(1) @binding(0) var<storage, read> models:    array<mat4x4<f32>>;
@group(1) @binding(1) var<storage, read> drawables: array<Drawable>;
@group(1) @binding(2) var<storage, read> visible:   array<u32>;

@group(2) @binding(0) var<storage, read> positions:  array<f32>;
@group(2) @binding(2) var<storage, read> geometries: array<GeometryDesc>;

struct VertexOutput {
    @builtin(position) clip_position: vec4<f32>,
    @location(0)       world_pos:     vec3<f32>,
}

@vertex
fn main(@builtin(instance_index) ii: u32,
        @builtin(vertex_index)   vi: u32) -> VertexOutput {
    let d   = drawables[visible[ii]];
    let geo = geometries[d.geometry_id];

    // vi is the local index (hardware index fetch, baseVertex 0).
    let pb = geo.position_base + vi * 3u;
    let position = vec3<f32>(positions[pb], positions[pb + 1u], positions[pb + 2u]);

    let world_pos = (models[d.transform_id] * vec4<f32>(position, 1.0)).xyz;

    var out: VertexOutput;
    out.clip_position = shadow.view_projection * vec4<f32>(world_pos, 1.0);
    out.world_pos = world_pos;
    return out;
}
