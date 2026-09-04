package geometries

import (
	"github.com/bluescreen10/pix/colors"
	"github.com/bluescreen10/pix/glm"
	"github.com/bluescreen10/pix/internal/mem"
)

// vertexAttributes is the interleaved per-vertex attribute record (16 bytes,
// scalar-packed). Mirrors the GLSL attribute stream layout.
type vertexAttributes struct {
	normal glm.Unorm10x3 // unit normal, [-1,1] remapped to the unsigned [0,1]
	color  colors.RGBA8
	uv     glm.Vec2f
}

// vertexSkin is the interleaved per-vertex skin record (16 bytes): 4 skeleton-
// relative joint indices + 4 unorm16 weights (normalized to sum 1 at pack time).
// Same 4-word shape as vertexAttributes, so it addresses identically in the GLSL.
type vertexSkin struct {
	joints  [4]uint16
	weights [4]uint16 // unorm16
}

// entry is the source of truth for one geometry. Attributes hold the original CPU
// bytes (retained for grow-repacking and GetAttributeData); the packed GPU stream
// bytes are reconstructed on demand. It holds each present stream's suballocation
// and the local bounds.
//
// derived marks a geometry with no CPU-side attribute bytes at all: a compute-
// skinning output range (see createSkinOutput), sized to mirror a source geometry's
// vertex count but filled by the GPU every frame instead of uploaded once. Its
// position/attribute presence is tracked directly (derivedHasAttr) rather than
// through has()/hasVertexAttrs(), which read attrs[].count — a derived entry only
// sets AttributePosition's count (for sizing) and carries no other attribute data.
type entry struct {
	attrs          [attributeCount]Attribute
	indices        []uint32
	derived        bool
	derivedHasAttr bool
	allocs         [streamCount]mem.Allocation

	boundingSphere glm.Sphere
}

// has reports whether an attribute is present (non-empty).
func (e *entry) has(t AttributeType) bool {
	return e.attrs[t].count > 0
}

// vec2/vec3/vec4/vec4u16 return typed read-only views over an attribute's stored bytes.
func (e *entry) vec2(t AttributeType) []glm.Vec2f {
	return fromBytes[glm.Vec2f](e.attrs[t].data, e.attrs[t].count)
}

func (e *entry) vec3(t AttributeType) []glm.Vec3f {
	return fromBytes[glm.Vec3f](e.attrs[t].data, e.attrs[t].count)
}

func (e *entry) vec4(t AttributeType) []glm.Vec4f {
	return fromBytes[glm.Vec4f](e.attrs[t].data, e.attrs[t].count)
}

func (e *entry) rgba(t AttributeType) []colors.RGBA32F {
	return fromBytes[colors.RGBA32F](e.attrs[t].data, e.attrs[t].count)
}

func (e *entry) vec4u16(t AttributeType) []glm.Vec4[uint16] {
	return fromBytes[glm.Vec4[uint16]](e.attrs[t].data, e.attrs[t].count)
}

func (e *entry) hasVertexAttrs() bool {
	return e.has(AttributeNormal) || e.has(AttributeColor) || e.has(AttributeUV)
}

// hasSkin reports whether both skin attributes are present. alloc() rejects one
// without the other, so this is also the single source of truth for FlagSkinned.
func (e *entry) hasSkin() bool {
	return e.has(AttributeSkinIndex) && e.has(AttributeSkinWeight)
}

func (e *entry) streamPresent(stream int) bool {
	switch stream {
	case streamPos:
		return true
	case streamAttr:
		if e.derived {
			return e.derivedHasAttr
		}
		return e.hasVertexAttrs()
	case streamIndex:
		return !e.derived // a derived (skin output) entry reuses its source's index range
	case streamSkin:
		return !e.derived && e.hasSkin()
	}
	return false
}

func (e *entry) flags() uint32 {
	var f uint32
	if e.has(AttributeNormal) {
		f |= FlagNormal
	}
	if e.has(AttributeUV) {
		f |= FlagUV
	}
	if e.has(AttributeColor) {
		f |= FlagColor
	}
	if e.hasSkin() {
		f |= FlagSkinned
	}
	return f
}

// bytes reconstructs the packed byte payload for a stream from the attributes. Never
// called for a derived entry (it has none — see growStream's derived special case).
func (e *entry) bytes(stream int) []byte {
	switch stream {
	case streamPos:
		return e.attrs[AttributePosition].data // raw f32x3, same layout as the stream
	case streamIndex:
		return toBytes(e.indices)
	case streamAttr:
		return e.packAttributes()
	case streamSkin:
		return e.packSkin()
	}
	return nil
}

func (e *entry) packAttributes() []byte {
	if !e.hasVertexAttrs() {
		return nil
	}
	n := e.attrs[AttributePosition].count
	normals, vertColors, uvs := e.vec3(AttributeNormal), e.rgba(AttributeColor), e.vec2(AttributeUV)
	attrs := make([]vertexAttributes, n)
	for i := 0; i < n; i++ {
		va := vertexAttributes{color: colors.RGBA8{255, 255, 255, 255}} // default white
		if i < len(normals) {
			// Unorm10x3 stores unsigned [0,1], so remap the signed normal here. The
			// other half of this codec is in scene_draw.vert.glsl ("* 2.0 - 1.0");
			// the two have to stay in step.
			nn := normals[i]
			va.normal = glm.Vec3f{nn[0]*0.5 + 0.5, nn[1]*0.5 + 0.5, nn[2]*0.5 + 0.5}.Unorm10x3()
		}
		if i < len(vertColors) {
			va.color = vertColors[i].RGBA8()
		}
		if i < len(uvs) {
			va.uv = uvs[i]
		}
		attrs[i] = va
	}
	return toBytes(attrs)
}

// packSkin builds the packed skin-record stream from the skin index/weight
// attributes: weights are normalized to sum 1 (an all-zero record falls back to
// full weight on joint 0) and quantized to unorm16.
func (e *entry) packSkin() []byte {
	if !e.hasSkin() {
		return nil
	}
	n := e.attrs[AttributePosition].count
	joints, weights := e.vec4u16(AttributeSkinIndex), e.vec4(AttributeSkinWeight)
	out := make([]vertexSkin, n)
	for i := 0; i < n; i++ {
		var j glm.Vec4[uint16]
		if i < len(joints) {
			j = joints[i]
		}
		w := glm.Vec4f{1, 0, 0, 0}
		if i < len(weights) {
			if sum := weights[i][0] + weights[i][1] + weights[i][2] + weights[i][3]; sum > 0 {
				inv := 1 / sum
				w = glm.Vec4f{weights[i][0] * inv, weights[i][1] * inv, weights[i][2] * inv, weights[i][3] * inv}
			}
		}
		out[i] = vertexSkin{
			joints:  [4]uint16{j[0], j[1], j[2], j[3]},
			weights: [4]uint16{quantizeUnorm16(w[0]), quantizeUnorm16(w[1]), quantizeUnorm16(w[2]), quantizeUnorm16(w[3])},
		}
	}
	return toBytes(out)
}

// quantizeUnorm16 clamps v to [0,1] and quantizes it to a 16-bit unsigned-normalized value.
func quantizeUnorm16(v float32) uint16 {
	if v < 0 {
		v = 0
	}
	if v > 1 {
		v = 1
	}
	return uint16(v*65535 + 0.5)
}
