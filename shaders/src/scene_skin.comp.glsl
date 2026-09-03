#version 460
#extension GL_EXT_buffer_reference : require
#extension GL_EXT_buffer_reference2 : require
#extension GL_EXT_scalar_block_layout : require
#extension GL_EXT_shader_explicit_arithmetic_types_int64 : require

// Compute pre-skinning. One thread per vertex: blend up to 4 joint matrices and
// write the result into a persistent per-SkinnedMesh output range in the shared
// position/attribute streams (allocated once at SkinnedMesh creation — see
// geometrySystem.createSkinOutput in geometry.go). Output is in skeleton-local
// space (joints already carry rootWorldInv * boneWorld * invBind — see
// Scene.syncSkinning), so the drawable's own transformID (the skeleton root node)
// applies the remaining world transform exactly like static geometry:
// scene_cull.comp / scene_draw.vert / scene_shadow.vert are untouched by skinning.
// One dispatch per skinned mesh (see Renderer.dispatchSkinning).
layout(local_size_x = 64) in;

struct GeoDesc {
    uint positionBase;
    uint attributeBase;
    uint indexBase;
    uint indexCount;
    uint flags;
    uint skinBase;
};

layout(buffer_reference, scalar) buffer PosBuf { float v[]; };
layout(buffer_reference, scalar) buffer AttrBuf { uint v[]; };
layout(buffer_reference, scalar) readonly buffer SkinBuf { uint v[]; };
layout(buffer_reference, scalar) readonly buffer DescBuf { GeoDesc v[]; };
layout(buffer_reference, scalar) readonly buffer JointBuf { mat4 v[]; };

layout(buffer_reference, scalar) readonly buffer SkinRoot {
    PosBuf pos;
    AttrBuf attr;
    SkinBuf skin;
    DescBuf descs;
    JointBuf joints;
    uint srcDesc;
    uint dstDesc;
    uint jointBase;
    uint vertexCount;
};
layout(push_constant) uniform PC { SkinRoot root; } pc;

const uint FLAG_NORMAL = 1u;
const uint FLAG_UV = 2u;
const uint FLAG_COLOR = 4u;

void main() {
    uint vi = gl_GlobalInvocationID.x;
    if (vi >= pc.root.vertexCount) return;

    GeoDesc src = pc.root.descs.v[pc.root.srcDesc];
    GeoDesc dst = pc.root.descs.v[pc.root.dstDesc];

    // Skin record: 4 words — joints packed two-per-word, then unorm16 weights two-
    // per-word (mirrors geometry.go's vertexSkin packing).
    uint sb = src.skinBase * 4u + vi * 4u;
    uint w0 = pc.root.skin.v[sb];
    uint w1 = pc.root.skin.v[sb + 1u];
    uint w2 = pc.root.skin.v[sb + 2u];
    uint w3 = pc.root.skin.v[sb + 3u];
    uvec4 j = uvec4(w0 & 0xFFFFu, w0 >> 16, w1 & 0xFFFFu, w1 >> 16);
    vec4 wt = vec4(float(w2 & 0xFFFFu), float(w2 >> 16), float(w3 & 0xFFFFu), float(w3 >> 16)) / 65535.0;

    mat4 m = wt.x * pc.root.joints.v[pc.root.jointBase + j.x]
           + wt.y * pc.root.joints.v[pc.root.jointBase + j.y]
           + wt.z * pc.root.joints.v[pc.root.jointBase + j.z]
           + wt.w * pc.root.joints.v[pc.root.jointBase + j.w];

    uint spb = src.positionBase + vi * 3u;
    vec3 p = vec3(pc.root.pos.v[spb], pc.root.pos.v[spb + 1u], pc.root.pos.v[spb + 2u]);
    vec3 outp = (m * vec4(p, 1.0)).xyz;

    uint dpb = dst.positionBase + vi * 3u;
    pc.root.pos.v[dpb] = outp.x;
    pc.root.pos.v[dpb + 1u] = outp.y;
    pc.root.pos.v[dpb + 2u] = outp.z;

    // Attributes: normal is rotated by the blended matrix's upper 3x3; color/uv
    // (if present) copy through unchanged — skinning never touches them.
    if ((src.flags & (FLAG_NORMAL | FLAG_UV | FLAG_COLOR)) != 0u) {
        uint sab = src.attributeBase * 4u + vi * 4u;
        uint dab = dst.attributeBase * 4u + vi * 4u;
        uint w = pc.root.attr.v[sab];
        if ((src.flags & FLAG_NORMAL) != 0u) {
            vec3 n = vec3(float(w & 0x3FFu), float((w >> 10) & 0x3FFu), float((w >> 20) & 0x3FFu)) / 1023.0 * 2.0 - 1.0;
            vec3 outn = normalize(mat3(m) * n);
            vec3 enc = clamp(outn * 0.5 + 0.5, 0.0, 1.0);
            uint ex = uint(enc.x * 1023.0 + 0.5);
            uint ey = uint(enc.y * 1023.0 + 0.5);
            uint ez = uint(enc.z * 1023.0 + 0.5);
            w = ex | (ey << 10) | (ez << 20);
        }
        pc.root.attr.v[dab] = w;
        pc.root.attr.v[dab + 1u] = pc.root.attr.v[sab + 1u];
        pc.root.attr.v[dab + 2u] = pc.root.attr.v[sab + 2u];
        pc.root.attr.v[dab + 3u] = pc.root.attr.v[sab + 3u];
    }
}
