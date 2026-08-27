package pix

import (
	"unsafe"

	"github.com/bluescreen10/pix/glm"
)

// PBR texture slots (index into the store's per-instance texture array).
const (
	pbrSlotColor  = 0
	pbrSlotNormal = 1
	pbrSlotMetal  = 2
	pbrSlotRough  = 3
	pbrSlots      = 4
)

// pbrRecord is the GPU material record for metallic-roughness PBR (64 bytes); matches
// the Material struct in scene_pbr.frag. POD — it lives directly in the store buffer.
// All maps share one sampler.
type pbrRecord struct {
	baseColor [4]float32
	emissive  [4]float32
	metallic  float32
	roughness float32
	flags     uint32
	sampler   uint32
	colorMap  uint32
	normalMap uint32
	metalMap  uint32
	roughMap  uint32
}

// PBRMaterial is a metallic-roughness physically-based material (Cook-Torrance) with
// optional base-color, normal, metallic and roughness maps.
type PBRMaterial struct {
	genericMaterial
}

// NewPBRMaterial creates a PBR material (4 texture slots: color, normal, metallic,
// roughness).
func (r *Renderer) NewPBRMaterial() *PBRMaterial {
	st := r.mats.store(ShaderPBR, uint32(unsafe.Sizeof(pbrRecord{})), pbrSlots, "PBR Materials")
	id := st.alloc()
	rc := int32(1)
	m := &PBRMaterial{genericMaterial{store: st, ref: Ref{id: id, gen: st.gens[id], refCount: &rc, owner: st}}}
	*m.rec() = pbrRecord{
		baseColor: [4]float32{1, 1, 1, 1}, roughness: 0.5,
		colorMap: noTextureIndex, normalMap: noTextureIndex, metalMap: noTextureIndex, roughMap: noTextureIndex,
	}
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

// setMap binds a texture into slot, updates the record's map index/flag, and stores
// the (shared) sampler. All maps use the same sampler.
func (m *PBRMaterial) setMap(slot int, t Texture, sampler uint32, idx *uint32, flag uint32) {
	m.store.setTexture(m.ref.id, slot, t)
	r := m.rec()
	r.sampler = sampler
	if t.Valid() {
		*idx, r.flags = t.index, r.flags|flag
	} else {
		*idx, r.flags = noTextureIndex, r.flags&^flag
	}
}

// SetColorMap binds the base-color (albedo) map; pass a zero Texture to clear it.
func (m *PBRMaterial) SetColorMap(t Texture, sampler uint32) {
	m.setMap(pbrSlotColor, t, sampler, &m.rec().colorMap, MatColorMap)
}

// SetNormalMap binds a tangent-space normal map (linear RGB, xyz in [0,1]).
func (m *PBRMaterial) SetNormalMap(t Texture, sampler uint32) {
	m.setMap(pbrSlotNormal, t, sampler, &m.rec().normalMap, MatNormalMap)
}

// SetMetallicMap binds a metallic map; its blue channel modulates the metallic factor
// (matches the glTF metallic-roughness texture convention, and works for grayscale).
func (m *PBRMaterial) SetMetallicMap(t Texture, sampler uint32) {
	m.setMap(pbrSlotMetal, t, sampler, &m.rec().metalMap, MatMetalMap)
}

// SetRoughnessMap binds a roughness map; its green channel modulates the roughness
// factor (glTF convention; works for grayscale too).
func (m *PBRMaterial) SetRoughnessMap(t Texture, sampler uint32) {
	m.setMap(pbrSlotRough, t, sampler, &m.rec().roughMap, MatRoughMap)
}

// ColorMap returns the bound base-color texture (or a zero handle).
func (m *PBRMaterial) ColorMap() Texture { return m.store.texture(m.ref.id, pbrSlotColor) }

// Ref returns another generic Material handle to the same instance (for meshes).
func (m *PBRMaterial) Ref() Material { return m.genericMaterial.Copy() }
