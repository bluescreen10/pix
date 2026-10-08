#version 460
#extension GL_EXT_buffer_reference : require
#extension GL_EXT_buffer_reference2 : require
#extension GL_EXT_scalar_block_layout : require
#extension GL_EXT_shader_explicit_arithmetic_types_int64 : require
#extension GL_GOOGLE_include_directive : require

#include "normal_matrix.glsl"

// Compute deformation: morph targets, then skinning. One thread per vertex: blend the
// source's morph targets into its bind pose, pose the result with up to 4 joint
// matrices, and write it into a persistent per-mesh output range in the shared
// position/attribute streams (allocated once at mesh creation — see
// Store.createDeformOutput in geometries/store.go).
//
// Morph targets come first because they are authored against the bind pose: a target
// that opens a mouth moves the jaw's vertices where the jaw was modelled, and skinning
// then carries the open mouth wherever the head has turned. Applied the other way,
// the deltas would point the way the head faced at rest.
//
// Skinned output is in skeleton-local space (joints already carry rootWorldInv *
// boneWorld * invBind — see Scene.updateSkinning), so the drawable's own transformID
// (the skeleton root node) applies the remaining world transform exactly like static
// geometry: scene_cull.comp / scene_draw.vert / scene_depth.vert are untouched by
// deformation. One dispatch per deformed mesh (see Renderer.encodeDeforms).
layout(local_size_x = 64) in;

// MORPHED and SKINNED select which deformations this pipeline applies; with neither,
// it copies the source unchanged — what a morphed mesh at rest draws.
layout(constant_id = 0) const bool MORPHED = true;
layout(constant_id = 1) const bool SKINNED = true;

struct GeoDesc {
    uint positionBase;
    uint attributeBase;
    uint indexBase;
    uint indexCount;
    uint flags;
    uint skinBase;
    uint morphBase;
    uint morphTargetCount;
};

struct MorphWeight {
    uint target;
    float weight;
};

layout(buffer_reference, scalar) buffer PosBuf { float v[]; };
layout(buffer_reference, scalar) buffer AttrBuf { uint v[]; };
layout(buffer_reference, scalar) readonly buffer SkinBuf { uint v[]; };
layout(buffer_reference, scalar) readonly buffer MorphBuf { uint v[]; };
layout(buffer_reference, scalar) readonly buffer DescBuf { GeoDesc v[]; };
layout(buffer_reference, scalar) readonly buffer JointBuf { mat4 v[]; };
layout(buffer_reference, scalar) readonly buffer MorphWeightBuf { MorphWeight v[]; };

layout(push_constant, scalar) uniform PC {
    PosBuf pos;
    AttrBuf attr;
    SkinBuf skin;
    MorphBuf morph;
    DescBuf descs;
    JointBuf joints;
    MorphWeightBuf morphWeights;
    uint srcDesc;
    uint dstDesc;
    uint jointBase;
    uint morphWeightBase;
    uint morphWeightCount;
    uint vertexCount;
} pc;

const uint FLAG_NORMAL = 1u;
const uint FLAG_UV = 2u;
const uint FLAG_COLOR = 4u;

// decodeNormal unpacks glm.Unorm10x3 and remaps it back to [-1,1]; encodeNormal is its
// inverse. The other half of this codec is entry.packAttributes in geometries.
vec3 decodeNormal(uint w) {
    return vec3(float(w & 0x3FFu), float((w >> 10) & 0x3FFu), float((w >> 20) & 0x3FFu)) / 1023.0 * 2.0 - 1.0;
}

uint encodeNormal(vec3 n) {
    vec3 enc = clamp(n * 0.5 + 0.5, 0.0, 1.0);
    uint ex = uint(enc.x * 1023.0 + 0.5);
    uint ey = uint(enc.y * 1023.0 + 0.5);
    uint ez = uint(enc.z * 1023.0 + 0.5);
    return ex | (ey << 10) | (ez << 20);
}

// decodeNormalDelta unpacks three signed 10-bit fields, each round(c/2 * 511). The
// other half of this codec is packNormalDelta in geometries/morph.go.
vec3 decodeNormalDelta(uint w) {
    int packed = int(w);
    ivec3 q = ivec3(bitfieldExtract(packed, 0, 10), bitfieldExtract(packed, 10, 10), bitfieldExtract(packed, 20, 10));
    return vec3(q) / 511.0 * 2.0;
}

// blendJoints is the vertex's skinning matrix: its up to 4 joints' matrices, weighted.
// Skin record: 4 words — joints packed two-per-word, then unorm16 weights two-per-word
// (mirrors entry.packSkin in geometries).
mat4 blendJoints(GeoDesc src, uint vi) {
    uint sb = src.skinBase * 4u + vi * 4u;
    uint w0 = pc.skin.v[sb];
    uint w1 = pc.skin.v[sb + 1u];
    uint w2 = pc.skin.v[sb + 2u];
    uint w3 = pc.skin.v[sb + 3u];
    uvec4 j = uvec4(w0 & 0xFFFFu, w0 >> 16, w1 & 0xFFFFu, w1 >> 16);
    vec4 wt = vec4(float(w2 & 0xFFFFu), float(w2 >> 16), float(w3 & 0xFFFFu), float(w3 >> 16)) / 65535.0;

    return wt.x * pc.joints.v[pc.jointBase + j.x]
         + wt.y * pc.joints.v[pc.jointBase + j.y]
         + wt.z * pc.joints.v[pc.jointBase + j.z]
         + wt.w * pc.joints.v[pc.jointBase + j.w];
}

void main() {
    uint vi = gl_GlobalInvocationID.x;
    if (vi >= pc.vertexCount) return;

    GeoDesc src = pc.descs.v[pc.srcDesc];
    GeoDesc dst = pc.descs.v[pc.dstDesc];
    bool hasNormal = (src.flags & FLAG_NORMAL) != 0u;
    bool hasAttributes = (src.flags & (FLAG_NORMAL | FLAG_UV | FLAG_COLOR)) != 0u;

    uint spb = src.positionBase + vi * 3u;
    vec3 p = vec3(pc.pos.v[spb], pc.pos.v[spb + 1u], pc.pos.v[spb + 2u]);
    uint sab = src.attributeBase * 4u + vi * 4u;
    uint normalWord = hasAttributes ? pc.attr.v[sab] : 0u;
    vec3 n = hasNormal ? decodeNormal(normalWord) : vec3(0.0);

    // Morph record: 4 words — the position delta as floats, then the packed normal
    // delta. Vertex vi of target t is record t*vertexCount + vi (see vertexMorph).
    if (MORPHED) {
        for (uint k = 0u; k < pc.morphWeightCount; k++) {
            MorphWeight mw = pc.morphWeights.v[pc.morphWeightBase + k];
            uint mb = (src.morphBase + mw.target * pc.vertexCount + vi) * 4u;
            p += mw.weight * vec3(uintBitsToFloat(pc.morph.v[mb]), uintBitsToFloat(pc.morph.v[mb + 1u]), uintBitsToFloat(pc.morph.v[mb + 2u]));
            if (hasNormal) {
                n += mw.weight * decodeNormalDelta(pc.morph.v[mb + 3u]);
            }
        }
    }

    if (SKINNED) {
        mat4 m = blendJoints(src, vi);
        p = (m * vec4(p, 1.0)).xyz;
        if (hasNormal) {
            n = normalMatrix(m) * n;
        }
    }

    uint dpb = dst.positionBase + vi * 3u;
    pc.pos.v[dpb] = p.x;
    pc.pos.v[dpb + 1u] = p.y;
    pc.pos.v[dpb + 2u] = p.z;

    // Attributes: the normal is re-encoded if anything turned it; color/uv (if present)
    // copy through unchanged — deformation never touches them.
    if (hasAttributes) {
        uint dab = dst.attributeBase * 4u + vi * 4u;
        if (hasNormal && (MORPHED || SKINNED)) {
            normalWord = encodeNormal(normalize(n));
        }
        pc.attr.v[dab] = normalWord;
        pc.attr.v[dab + 1u] = pc.attr.v[sab + 1u];
        pc.attr.v[dab + 2u] = pc.attr.v[sab + 2u];
        pc.attr.v[dab + 3u] = pc.attr.v[sab + 3u];
    }
}
