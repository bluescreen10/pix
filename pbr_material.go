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

// pbrRecord is the GPU material record for metallic-roughness PBR (80 bytes); matches
// the Material struct in scene_pbr.frag. POD — it lives directly in the store buffer.
// Each map carries its own bindless sampler index.
type pbrRecord struct {
	color         glm.RGBA32F
	emissive      glm.RGBA32F
	metallic      float32
	roughness     float32
	transmission  float32 // 0 = opaque; >0 = see-through (glass), needs alpha blend
	flags         uint32
	colorMap      uint32
	colorSampler  uint32
	normalMap     uint32
	normalSampler uint32
	metalMap      uint32
	metalSampler  uint32
	roughMap      uint32
	roughSampler  uint32
}

// PBRMaterial is a metallic-roughness physically-based material (Cook-Torrance) with
// optional base-color, normal, metallic and roughness maps.
type PBRMaterial struct {
	genericMaterial
}

// NewPBRMaterial creates a PBR material (4 texture slots: color, normal, metallic,
// roughness).
func (r *Renderer) NewPBRMaterial() *PBRMaterial {
	st := r.materials.store(ShaderPBR, uint32(unsafe.Sizeof(pbrRecord{})), pbrSlots, "PBR Materials")
	id := st.alloc()
	rc := int32(1)
	m := &PBRMaterial{genericMaterial{store: st, ref: Ref{id: id, gen: st.gens[id], refCount: &rc, owner: st}}}
	*m.record() = pbrRecord{
		color: glm.RGBA32F{1, 1, 1, 1}, roughness: 0.5,
		colorMap: noTextureIndex, normalMap: noTextureIndex, metalMap: noTextureIndex, roughMap: noTextureIndex,
	}
	return m
}

func (m *PBRMaterial) record() *pbrRecord {
	return (*pbrRecord)(m.store.record(m.ref.id))
}

func (m *PBRMaterial) Color() glm.RGBA32F {
	return m.record().color
}

func (m *PBRMaterial) SetColor(color glm.RGBA32F) {
	m.record().color = color
}

func (m *PBRMaterial) Metallic() float32 {
	return m.record().metallic
}

func (m *PBRMaterial) SetMetallic(v float32) {
	m.record().metallic = v
}

func (m *PBRMaterial) Roughness() float32 {
	return m.record().roughness
}

func (m *PBRMaterial) SetRoughness(v float32) {
	m.record().roughness = v
}

// Transmission returns the transmission factor [0,1] (glass-like see-through).
func (m *PBRMaterial) Transmission() float32 {
	return m.record().transmission
}

// SetTransmission sets how much light passes through the surface [0,1]. A value > 0
// makes the material transparent (approximate glass; also call SetBlend(BlendAlpha),
// which the loader does automatically for KHR_materials_transmission).
func (m *PBRMaterial) SetTransmission(v float32) {
	m.record().transmission = v
}

func (m *PBRMaterial) Emissive() glm.RGBA32F {
	return m.record().emissive
}

func (m *PBRMaterial) SetEmissive(color glm.RGBA32F) {
	m.record().emissive = color
}

// setMap binds a texture into slot and updates the record's map index, per-map
// sampler, and presence flag. Clearing (zero Texture) leaves the sampler as-is.
func (m *PBRMaterial) setMap(slot int, t Texture, sampler uint32, idx, samp *uint32, flag uint32) {
	m.store.setTexture(m.ref.id, slot, t)
	r := m.record()
	if t.Valid() {
		*idx, *samp, r.flags = t.index, sampler, r.flags|flag
	} else {
		*idx, r.flags = noTextureIndex, r.flags&^flag
	}
}

// SetColorMap binds the base-color (albedo) map; pass a zero Texture to clear it.
func (m *PBRMaterial) SetColorMap(t Texture, sampler uint32) {
	r := m.record()
	m.setMap(pbrSlotColor, t, sampler, &r.colorMap, &r.colorSampler, MatColorMap)
}

// SetNormalMap binds a tangent-space normal map (linear RGB, xyz in [0,1]).
func (m *PBRMaterial) SetNormalMap(t Texture, sampler uint32) {
	r := m.record()
	m.setMap(pbrSlotNormal, t, sampler, &r.normalMap, &r.normalSampler, MatNormalMap)
}

// SetMetallicMap binds a metallic map; its blue channel modulates the metallic factor
// (matches the glTF metallic-roughness texture convention, and works for grayscale).
func (m *PBRMaterial) SetMetallicMap(t Texture, sampler uint32) {
	r := m.record()
	m.setMap(pbrSlotMetal, t, sampler, &r.metalMap, &r.metalSampler, MatMetalMap)
}

// SetRoughnessMap binds a roughness map; its green channel modulates the roughness
// factor (glTF convention; works for grayscale too).
func (m *PBRMaterial) SetRoughnessMap(t Texture, sampler uint32) {
	r := m.record()
	m.setMap(pbrSlotRough, t, sampler, &r.roughMap, &r.roughSampler, MatRoughMap)
}

// Per-map sampler getters/setters (bindless sampler heap indices).
func (m *PBRMaterial) ColorMapSampler() uint32 {
	return m.record().colorSampler
}

func (m *PBRMaterial) SetColorMapSampler(s uint32) {
	m.record().colorSampler = s
}

func (m *PBRMaterial) NormalMapSampler() uint32 {
	return m.record().normalSampler
}

func (m *PBRMaterial) SetNormalMapSampler(s uint32) {
	m.record().normalSampler = s
}

func (m *PBRMaterial) MetallicMapSampler() uint32 {
	return m.record().metalSampler
}

func (m *PBRMaterial) SetMetallicMapSampler(s uint32) {
	m.record().metalSampler = s
}

func (m *PBRMaterial) RoughnessMapSampler() uint32 {
	return m.record().roughSampler
}

func (m *PBRMaterial) SetRoughnessMapSampler(s uint32) {
	m.record().roughSampler = s
}

// ColorMap returns the bound base-color texture (or a zero handle).
func (m *PBRMaterial) ColorMap() Texture {
	return m.store.texture(m.ref.id, pbrSlotColor)
}

// Ref returns another generic Material handle to the same instance (for meshes).
func (m *PBRMaterial) Ref() Material {
	return m.genericMaterial.Copy()
}
