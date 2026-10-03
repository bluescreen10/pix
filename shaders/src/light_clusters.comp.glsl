#version 460
#extension GL_EXT_buffer_reference : require
#extension GL_EXT_buffer_reference2 : require
#extension GL_EXT_scalar_block_layout : require
#extension GL_EXT_nonuniform_qualifier : require
#extension GL_EXT_shader_explicit_arithmetic_types_int64 : require
#extension GL_GOOGLE_include_directive : require

// light_clusters — lists, for every cell of the main view's cluster grid, the point and
// spot lights whose range reaches into it (see the cluster layout in lighting.glsl).
// One thread per cell: it bounds its cell with a box in view space, then tests every
// light's bounding sphere against that box.
//
// The lights are tested a workgroup's worth at a time. Each thread moves one light
// into view space and leaves its sphere in shared memory, and then every thread tests
// all of them, so a light is transformed once per workgroup rather than once per cell.
#define PIX_NO_FRAGMENT_FOG
#include "lighting.glsl"

#define GROUP_SIZE 64u
layout(local_size_x = GROUP_SIZE) in;

layout(buffer_reference, scalar) writeonly buffer ClusterCells { uint v[]; };

// Matches lightClustersRoot in light_clusters.go.
layout(push_constant, scalar) uniform PC {
    // view takes world space to the main view's; inverseProjection takes its clip space
    // (with the same Y flip lightCluster projects with) back to view space.
    mat4 view;
    mat4 inverseProjection;
    LightBuf lights;
    ClusterCells cells;
} pc;

shared vec4 spheres[GROUP_SIZE];

// viewCorner is the view-space point at depth along the view, on the line through the
// view volume at ndc. The line runs from the near plane (depth 1, reversed) to the far
// plane (0), which holds for orthographic projections as well as perspective ones.
vec3 viewCorner(vec2 ndc, float depth) {
    vec4 near = pc.inverseProjection * vec4(ndc, 1.0, 1.0);
    vec4 far = pc.inverseProjection * vec4(ndc, 0.0, 1.0);
    vec3 a = near.xyz / near.w;
    vec3 b = far.xyz / far.w;
    float t = (depth + a.z) / (a.z - b.z); // view space looks down -z
    return mix(a, b, t);
}

// sliceDepth is the view depth where slice z begins: the inverse of lightCluster's
// slicing.
float sliceDepth(LightBuf L, float z) {
    return exp((z - L.clusterSliceBias) / L.clusterSliceScale);
}

// touchesBox reports whether the sphere s reaches into the box lo..hi.
bool touchesBox(vec4 s, vec3 lo, vec3 hi) {
    vec3 d = s.xyz - clamp(s.xyz, lo, hi);
    return dot(d, d) <= s.w * s.w;
}

// spotSphere bounds what a spot light lights — the part of its range sphere inside its
// cone — more tightly than the range sphere does when the cone is narrow. Below 45° the
// sphere through the apex and the cap's rim holds it all; up to 90°, the sphere about
// the rim's centre does; past that, the range sphere is already the tightest.
vec4 spotSphere(SpotLight sl) {
    float range = sl.pos.w;
    float cosOuter = sl.dir.w;
    if (cosOuter >= 0.70710678) {
        float radius = range / (2.0 * cosOuter);
        return vec4(sl.pos.xyz + sl.dir.xyz * radius, radius);
    }
    if (cosOuter > 0.0) {
        float sinOuter = sqrt(1.0 - cosOuter * cosOuter);
        return vec4(sl.pos.xyz + sl.dir.xyz * (range * cosOuter), range * sinOuter);
    }
    return vec4(sl.pos.xyz, range);
}

void main() {
    LightBuf L = pc.lights;
    uint cellCount = CLUSTER_X * CLUSTER_Y * CLUSTER_Z;
    uint id = gl_GlobalInvocationID.x;
    // A thread past the last cell still takes part in loading lights: every thread of
    // the group has to reach every barrier.
    bool hasCell = id < cellCount;
    uint cellID = min(id, cellCount - 1u);

    uint x = cellID % CLUSTER_X;
    uint y = (cellID / CLUSTER_X) % CLUSTER_Y;
    uint z = cellID / (CLUSTER_X * CLUSTER_Y);
    vec2 ndcLo = vec2(x, y) / vec2(CLUSTER_X, CLUSTER_Y) * 2.0 - 1.0;
    vec2 ndcHi = vec2(x + 1u, y + 1u) / vec2(CLUSTER_X, CLUSTER_Y) * 2.0 - 1.0;
    float nearDepth = sliceDepth(L, float(z));
    float farDepth = sliceDepth(L, float(z + 1u));
    vec3 lo = vec3(1e30);
    vec3 hi = vec3(-1e30);
    for (uint c = 0u; c < 4u; c++) {
        vec2 ndc = vec2((c & 1u) != 0u ? ndcHi.x : ndcLo.x, (c & 2u) != 0u ? ndcHi.y : ndcLo.y);
        vec3 a = viewCorner(ndc, nearDepth);
        vec3 b = viewCorner(ndc, farDepth);
        lo = min(lo, min(a, b));
        hi = max(hi, max(a, b));
    }
    // The first slice reaches back to the eye and the last out to infinity: lightCluster
    // clamps a position nearer or farther than the grid into them.
    if (z == 0u) {
        hi.z = max(hi.z, 0.0);
    }
    if (z == CLUSTER_Z - 1u) {
        lo.z = -1e30;
    }

    uint base = cellID * CLUSTER_STRIDE;
    uint pointCount = 0u;
    for (uint first = 0u; first < L.numPoint; first += GROUP_SIZE) {
        uint li = first + gl_LocalInvocationID.x;
        if (li < L.numPoint) {
            PointLight pl = L.points.v[li];
            spheres[gl_LocalInvocationID.x] = vec4((pc.view * vec4(pl.pos.xyz, 1.0)).xyz, pl.pos.w);
        }
        barrier();
        uint n = min(GROUP_SIZE, L.numPoint - first);
        for (uint i = 0u; i < n && hasCell; i++) {
            if (pointCount < CLUSTER_CAPACITY && touchesBox(spheres[i], lo, hi)) {
                pc.cells.v[base + 1u + pointCount] = first + i;
                pointCount++;
            }
        }
        barrier();
    }

    uint spotCount = 0u;
    for (uint first = 0u; first < L.numSpot; first += GROUP_SIZE) {
        uint li = first + gl_LocalInvocationID.x;
        if (li < L.numSpot) {
            vec4 s = spotSphere(L.spots.v[li]);
            spheres[gl_LocalInvocationID.x] = vec4((pc.view * vec4(s.xyz, 1.0)).xyz, s.w);
        }
        barrier();
        uint n = min(GROUP_SIZE, L.numSpot - first);
        for (uint i = 0u; i < n && hasCell; i++) {
            if (pointCount + spotCount < CLUSTER_CAPACITY && touchesBox(spheres[i], lo, hi)) {
                pc.cells.v[base + 1u + pointCount + spotCount] = first + i;
                spotCount++;
            }
        }
        barrier();
    }

    if (hasCell) {
        pc.cells.v[base] = pointCount | (spotCount << 16);
    }
}
