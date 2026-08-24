struct Camera {
    view_projection:     mat4x4<f32>,
    inv_view_projection: mat4x4<f32>,
    sky_inv_view_proj:   mat4x4<f32>,
    position:            vec4<f32>,
}

// Must match the Go drawable struct (48 bytes).
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

struct GeometryDesc {
    position_base:  u32,
    attribute_base: u32,
    index_base:     u32,
    index_count:    u32,
    flags:          u32,
    _pad:           u32,
}

// Interleaved per-vertex attributes (normal/color/uv). Must match Go's
// vertexAttributes (16 bytes).
struct VertexAttributes {
    normal: u32,        // 10-10-10-2, components mapped [-1,1] -> [0,1]
    color:  u32,        // RGBA8
    uv:     vec2<f32>,
}

fn unpack_normal(p: u32) -> vec3<f32> {
    let x = f32((p >>  0u) & 0x3FFu) / 1023.0 * 2.0 - 1.0;
    let y = f32((p >> 10u) & 0x3FFu) / 1023.0 * 2.0 - 1.0;
    let z = f32((p >> 20u) & 0x3FFu) / 1023.0 * 2.0 - 1.0;
    return vec3<f32>(x, y, z);
}

fn unpack_position(vertex_index: u32) -> vec3<f32> {
    let x = positions[vertex_index];
    let y = positions[vertex_index+1u];
    let z = positions[vertex_index+2u];
    return vec3<f32>(x,y,z);
}

// normal_matrix builds the inverse-transpose of the model's upper-left 3x3 from
// its column cofactors (cross products), avoiding a separate inv-model buffer.
// It's correct up to a positive scale, which the caller's normalize() removes.
fn normal_matrix(m: mat4x4<f32>) -> mat3x3<f32> {
    let c0 = m[0].xyz;
    let c1 = m[1].xyz;
    let c2 = m[2].xyz;
    return mat3x3<f32>(cross(c1, c2), cross(c2, c0), cross(c0, c1));
}

struct VertexOutput {
    @builtin(position) clip_position: vec4<f32>,
    @location(0) v_world_pos: vec3<f32>,
    @location(1) v_uv: vec2<f32>,
    @location(2) v_normal: vec3<f32>,
}

@group(0) @binding(0) var<uniform>       camera:     Camera;

@group(2) @binding(0) var<storage, read> models:    array<mat4x4<f32>>;
@group(2) @binding(1) var<storage, read> drawables: array<Drawable>;
// Compacted survivors from the cull pass; bound per batch at its region, so
// instance_index is 0-based within the batch.
@group(2) @binding(2) var<storage, read> visible:   array<u32>;

// Geometry set: position stream + packed attribute stream + descriptors.
// Indices are a hardware index buffer, so vertex_index is already the local index.
@group(3) @binding(0) var<storage, read> positions:  array<f32>;
@group(3) @binding(1) var<storage, read> attributes: array<VertexAttributes>;
@group(3) @binding(2) var<storage, read> geometries: array<GeometryDesc>;

@vertex
fn main(@builtin(instance_index) instance_index: u32,
        @builtin(vertex_index)   vertex_index: u32) -> VertexOutput {
    var out: VertexOutput;

    let d   = drawables[visible[instance_index]];
    let geo = geometries[d.geometry_id];

    // vertex_index is the local index (hardware index fetch, baseVertex 0).
    let vi = vertex_index;

    let pb = geo.position_base + vi * 3u;
    let position = unpack_position(pb);

    let model = models[d.transform_id];

    let world_pos = model * vec4<f32>(position, 1.0);
    out.v_world_pos   = world_pos.xyz;
    out.clip_position = camera.view_projection * world_pos;
    out.v_uv = attributes[geo.attribute_base + vi].uv;
    
    let normal = unpack_normal(attributes[geo.attribute_base + vi].normal);
    out.v_normal = normalize(normal_matrix(model) * normal);

    return out;
}
