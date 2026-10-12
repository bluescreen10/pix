#version 460
#extension GL_EXT_buffer_reference : require
#extension GL_EXT_buffer_reference2 : require
#extension GL_EXT_scalar_block_layout : require
#extension GL_EXT_shader_explicit_arithmetic_types_int64 : require

// GPU-driven frustum cull + batch compaction. One thread per drawable: transform
// its local bounding sphere to world, frustum-test, and on survival bump its
// batch's indirect instanceCount and append its index into the batch's region of
// the shared visible buffer. Mirrors pix's cull.wesl on the bindless gpu.
//
// LOD (see the LOD spec, project memory): a drawable with lodID != 0 belongs to a
// group of sibling records (one per level, sharing lodID and transformID) — at most
// one survives per object per frame. Every sibling redundantly runs the same
// deterministic selectLevel(dist, prevLevel, ...) and only proceeds past the frustum
// test if it agrees it's the winner; this avoids needing the N sibling threads to
// coordinate with each other directly. prevLevel (persisted in PrevLevelBuf, keyed by
// transformID) is last frame's winner, read here before this frame's winner is known
// and written by it — so LOD selection this frame acts on last frame's decision,
// mirroring how engines pre-select a LOD before animating/skinning it (see the skin
// compute shader's matching gate) rather than evaluating every level and discarding
// the rest.
//
// TODO: LOD selection currently re-evaluates every LOD-tagged drawable's distance
// every frame. Dispatching this at a lower rate (e.g. once every N frames, like
// Unreal's animation update-rate optimization for distant actors) would cut cull cost
// for scenes with many LOD objects — deferred until it's actually a measured cost.
layout(local_size_x = 64) in;

struct Drawable {
    vec4 bounds;      // xyz local center, w radius
    uint transformID;
    uint geometryID;
    uint materialID;
    uint batchID;
    uint flags;
    uint lodID;
    uint lodLevel;
};
struct IndirectCmd { uint indexCount; uint instanceCount; uint firstIndex; int vertexOffset; uint firstInstance; };
struct LOD {
    float boundaries[3]; // ascending; boundaries[i] is level i's far edge, i < levelCount-1
    float hysteresis;
    uint levelCount;
    uint pad0, pad1;
};

layout(buffer_reference, scalar) readonly buffer DrawableBuf { Drawable v[]; };
layout(buffer_reference, scalar) readonly buffer ModelBuf { mat4 v[]; };
layout(buffer_reference, scalar) buffer IndirectBuf { IndirectCmd v[]; };
layout(buffer_reference, scalar) buffer VisibleBuf { uint v[]; };
layout(buffer_reference, scalar) readonly buffer LodBuf { LOD v[]; };
layout(buffer_reference, scalar) buffer PrevLevelBuf { uint v[]; };

// Pushed inline rather than behind a device address: it fits in push constants on
// every backend, so the shader reads its parameters directly instead of chasing a
// pointer to reach them. Fields that are themselves addresses stay addresses — those
// point at unbounded arrays, so that indirection is inherent.
layout(push_constant, scalar) uniform PC {
    DrawableBuf drawables;
    ModelBuf models;
    IndirectBuf indirect;
    VisibleBuf visible;
    LodBuf lods;
    PrevLevelBuf prevLevel;
    vec4 eye;
    uint count;
    uint castersOnly;
    vec4 planes[6];
} pc;

const uint FLAG_CASTS_SHADOW = 2u;

// selectLevel picks the level dist falls into, favoring stickiness: prevLevel's own
// band (widened by hysteresis on both edges) is checked FIRST, before the plain
// ascending scan of every level's unwidened band. Checking prevLevel first (rather
// than widening it in place inside the ascending scan) matters for levels other than
// 0 — an ascending scan would hit some lower, unwidened level's band before ever
// reaching a higher-indexed prevLevel's widened near edge, since adjacent levels'
// bands necessarily touch at their shared boundary; widening only the far edge would
// ever take effect. Checking prevLevel unconditionally first fixes that
// asymmetrically for both edges, on any level. Every sibling level-thread of one
// object computes this identically off the same (dist, prevLevel), so exactly one of
// them ends up agreeing it's the current level, without the threads needing to
// coordinate directly. See the LOD spec for why this specific formulation avoids the
// race a naive per-thread "am I in range" check would have near a boundary.
uint selectLevel(float dist, uint prevLevel, LOD le) {
    if (prevLevel < le.levelCount) {
        float nearB = (prevLevel == 0u) ? 0.0 : le.boundaries[prevLevel - 1u];
        float farB = (prevLevel + 1u < le.levelCount) ? le.boundaries[prevLevel] : 3.4e38;
        nearB = max(0.0, nearB - le.hysteresis);
        farB += le.hysteresis;
        if (dist >= nearB && dist < farB) return prevLevel;
    }
    for (uint i = 0u; i < le.levelCount; i++) {
        float nearB = (i == 0u) ? 0.0 : le.boundaries[i - 1u];
        float farB = (i + 1u < le.levelCount) ? le.boundaries[i] : 3.4e38;
        if (dist >= nearB && dist < farB) return i;
    }
    return le.levelCount - 1u;
}

void main() {
    uint i = gl_GlobalInvocationID.x;
    if (i >= pc.count) return;
    Drawable d = pc.drawables.v[i];
    if (pc.castersOnly != 0u && (d.flags & FLAG_CASTS_SHADOW) == 0u) return;

    mat4 m = pc.models.v[d.transformID];
    vec3 center = (m * vec4(d.bounds.xyz, 1.0)).xyz;
    float s = max(length(m[0].xyz), max(length(m[1].xyz), length(m[2].xyz)));
    float radius = d.bounds.w * s;
    for (int p = 0; p < 6; p++) {
        vec4 pl = pc.planes[p];
        if (dot(pl.xyz, center) + pl.w < -radius) return;
    }

    uint bid = d.batchID;
    if (d.lodID != 0u) {
        LOD le = pc.lods.v[d.lodID];
        uint prev = pc.prevLevel.v[d.transformID];
        float dist = distance(center, pc.eye.xyz);
        uint selected = selectLevel(dist, prev, le);
        if (selected != d.lodLevel) return;
        pc.prevLevel.v[d.transformID] = selected;
    }

    // A batch's region of the visible buffer starts at its own firstInstance: the draw
    // reads its instances from there, so the cull must write them there, and the
    // command already carries the number — no separate table of region bases needed.
    uint slot = atomicAdd(pc.indirect.v[bid].instanceCount, 1u);
    pc.visible.v[pc.indirect.v[bid].firstInstance + slot] = i;
}
