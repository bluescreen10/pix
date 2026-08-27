package pix

import "github.com/bluescreen10/pix/glm"

// pbrRecord is the GPU material record for metallic-roughness PBR materials (scalar,
// 48 bytes); matches the Material struct in scene_pbr.frag.
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

// PBRMaterial is a metallic-roughness physically-based material (Cook-Torrance).
type PBRMaterial struct {
	genericMaterial
	slab *materialSlab[pbrRecord]
}

// NewPBRMaterial creates a PBR material (1 texture slot: the base-color map).
// Defaults to a white dielectric at mid roughness.
func (r *Renderer) NewPBRMaterial() *PBRMaterial {
	s := r.mats.pbrStore()
	id := s.alloc(pbrRecord{baseColor: [4]float32{1, 1, 1, 1}, roughness: 0.5, colorMap: noTextureIndex})
	rc := int32(1)
	return &PBRMaterial{
		genericMaterial: genericMaterial{store: s, ref: Ref{id: id, gen: s.gens[id], refCount: &rc, owner: s}},
		slab:            s,
	}
}

func (m *PBRMaterial) rec() *pbrRecord { return m.slab.get(m.ref.id) }

// Color returns the base color (rgba).
func (m *PBRMaterial) Color() glm.RGBA32F { return glm.RGBA32F(m.rec().baseColor) }

// SetColor sets the base color (rgba).
func (m *PBRMaterial) SetColor(c glm.RGBA32F) { m.rec().baseColor = c; m.slab.touch() }

// Metallic returns the metallic factor [0,1].
func (m *PBRMaterial) Metallic() float32 { return m.rec().metallic }

// SetMetallic sets the metallic factor [0,1].
func (m *PBRMaterial) SetMetallic(v float32) { m.rec().metallic = v; m.slab.touch() }

// Roughness returns the roughness factor [0,1].
func (m *PBRMaterial) Roughness() float32 { return m.rec().roughness }

// SetRoughness sets the roughness factor [0,1].
func (m *PBRMaterial) SetRoughness(v float32) { m.rec().roughness = v; m.slab.touch() }

// Emissive returns the emissive color.
func (m *PBRMaterial) Emissive() glm.RGB32F {
	e := m.rec().emissive
	return glm.RGB32F{e[0], e[1], e[2]}
}

// SetEmissive sets the emissive color.
func (m *PBRMaterial) SetEmissive(c glm.RGB32F) {
	m.rec().emissive = [4]float32{c[0], c[1], c[2], 0}
	m.slab.touch()
}

// ColorMap returns the bound base-color texture (or a zero handle).
func (m *PBRMaterial) ColorMap() Texture { return m.slab.texture(m.ref.id, 0) }

// SetColorMap binds a base-color texture sampled with sampler (0 = default sampler).
func (m *PBRMaterial) SetColorMap(t Texture, sampler uint32) {
	m.slab.setTexture(m.ref.id, 0, t)
	r := m.rec()
	if t.Valid() {
		r.colorMap, r.sampler, r.flags = t.index, sampler, r.flags|MatColorMap
	} else {
		r.colorMap, r.flags = noTextureIndex, r.flags&^MatColorMap
	}
}

// Ref returns another generic Material handle to the same instance (for meshes).
func (m *PBRMaterial) Ref() Material { return m.genericMaterial.Copy() }
