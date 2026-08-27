package pix

import (
	"unsafe"

	"github.com/bluescreen10/pix/glm"
)

// pbrRecord is the GPU material record for metallic-roughness PBR (64 bytes); matches
// the Material struct in scene_pbr.frag. POD — it lives directly in the store buffer.
type pbrRecord struct {
	baseColor [4]float32
	emissive  [4]float32
	metallic  float32
	roughness float32
	colorMap  uint32
	sampler   uint32
	flags     uint32
	_, _, _   uint32
}

// PBRMaterial is a metallic-roughness physically-based material (Cook-Torrance). It
// embeds genericMaterial (satisfying Material) and adds typed accessors that write
// straight into its record in the store's mapped buffer.
type PBRMaterial struct {
	genericMaterial
}

// NewPBRMaterial creates a PBR material (1 texture slot: the base-color map).
func (r *Renderer) NewPBRMaterial() *PBRMaterial {
	st := r.mats.store(ShaderPBR, uint32(unsafe.Sizeof(pbrRecord{})), 1, "PBR Materials")
	id := st.alloc()
	rc := int32(1)
	m := &PBRMaterial{genericMaterial{store: st, ref: Ref{id: id, gen: st.gens[id], refCount: &rc, owner: st}}}
	*m.rec() = pbrRecord{baseColor: [4]float32{1, 1, 1, 1}, roughness: 0.5, colorMap: noTextureIndex}
	return m
}

func (m *PBRMaterial) rec() *pbrRecord { return (*pbrRecord)(m.store.record(m.ref.id)) }

func (m *PBRMaterial) Color() glm.RGBA32F     { return glm.RGBA32F(m.rec().baseColor) }
func (m *PBRMaterial) SetColor(c glm.RGBA32F) { m.rec().baseColor = c }

func (m *PBRMaterial) Metallic() float32     { return m.rec().metallic }
func (m *PBRMaterial) SetMetallic(v float32) { m.rec().metallic = v }

func (m *PBRMaterial) Roughness() float32     { return m.rec().roughness }
func (m *PBRMaterial) SetRoughness(v float32) { m.rec().roughness = v }

func (m *PBRMaterial) Emissive() glm.RGB32F {
	e := m.rec().emissive
	return glm.RGB32F{e[0], e[1], e[2]}
}
func (m *PBRMaterial) SetEmissive(c glm.RGB32F) { m.rec().emissive = [4]float32{c[0], c[1], c[2], 0} }

// ColorMap returns the bound base-color texture (or a zero handle).
func (m *PBRMaterial) ColorMap() Texture { return m.store.texture(m.ref.id, 0) }

// SetColorMap binds a base-color texture sampled with sampler (0 = default sampler).
func (m *PBRMaterial) SetColorMap(t Texture, sampler uint32) {
	m.store.setTexture(m.ref.id, 0, t)
	r := m.rec()
	if t.Valid() {
		r.colorMap, r.sampler, r.flags = t.index, sampler, r.flags|MatColorMap
	} else {
		r.colorMap, r.flags = noTextureIndex, r.flags&^MatColorMap
	}
}

// Ref returns another generic Material handle to the same instance (for meshes).
func (m *PBRMaterial) Ref() Material { return m.genericMaterial.Copy() }
