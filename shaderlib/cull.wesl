// GPU frustum culling. One invocation per drawable: transform the local bounding
// sphere to world space, test it against the 6 frustum planes, and for survivors
// append the drawable index into its batch's region of the visible buffer while
// bumping that batch's indirect instance_count.

struct Drawable {
    bounds:       vec4<f32>,   // xyz = local center, w = local radius
    instance:     u32,
    transform_id: u32,
    geometry_id:  u32,
    material_id:  u32,
    skeleton_id:  u32,
    owner_node:   u32,
    flags:        u32,
    batch_id:     u32,
}

struct DrawIndexedIndirect {
    index_count:    u32,
    instance_count: atomic<u32>,
    first_index:    u32,
    base_vertex:    i32,
    first_instance: u32,
}

struct CullParams {
    planes:       array<vec4<f32>, 6>,
    count:        u32,
    casters_only: u32, // 1 = only drawables with the cast-shadow flag
}

const FLAG_CASTS_SHADOW: u32 = 2u; // drawableCastsShadow (1 << 1)

@group(0) @binding(0) var<storage, read>       drawables:     array<Drawable>;
@group(0) @binding(1) var<storage, read>       models:        array<mat4x4<f32>>;
@group(0) @binding(2) var<uniform>             params:        CullParams;
@group(0) @binding(3) var<storage, read_write> indirect_args: array<DrawIndexedIndirect>;
@group(0) @binding(4) var<storage, read>       region_base:   array<u32>;
@group(0) @binding(5) var<storage, read_write> visible:       array<u32>;

@compute @workgroup_size(64)
fn main(@builtin(global_invocation_id) gid: vec3<u32>) {
    let i = gid.x;
    if (i >= params.count) {
        return;
    }

    let d = drawables[i];
    if (params.casters_only != 0u && (d.flags & FLAG_CASTS_SHADOW) == 0u) {
        return;
    }

    let m = models[d.transform_id];

    let center = (m * vec4<f32>(d.bounds.xyz, 1.0)).xyz;
    let scale = max(length(m[0].xyz), max(length(m[1].xyz), length(m[2].xyz)));
    let radius = d.bounds.w * scale;

    for (var p = 0u; p < 6u; p = p + 1u) {
        let pl = params.planes[p];
        if (dot(pl.xyz, center) + pl.w < -radius) {
            return;
        }
    }

    let b = d.batch_id;
    let slot = atomicAdd(&indirect_args[b].instance_count, 1u);
    visible[region_base[b] + slot] = i;
}
