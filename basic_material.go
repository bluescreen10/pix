package pix

import (
	"unsafe"

	"github.com/bluescreen10/pix/glm"
)

// basicRecord is the GPU material record for unlit materials (48 bytes); matches the
// Material struct in scene_basic.frag.
type basicRecord struct {
	color        glm.RGBA32F
	emissive     glm.RGBA32F
	colorMap     uint32
	colorSampler uint32
	flags        uint32
	_            uint32
}

// BasicMaterial is an unlit material: base color × vertex color × color map (+
// emissive), ignoring lights.
type BasicMaterial struct {
	genericMaterial
}

// NewBasicMaterial creates an unlit material (1 texture slot: the color map).
func (r *Renderer) NewBasicMaterial() *BasicMaterial {
	st := r.materials.store(ShaderBasic, uint32(unsafe.Sizeof(basicRecord{})), 1, "Basic Materials")
	id := st.alloc()
	rc := int32(1)
	m := &BasicMaterial{genericMaterial{store: st, ref: Ref{id: id, gen: st.gens[id], refCount: &rc, owner: st}}}
	*m.record() = basicRecord{color: [4]float32{1, 1, 1, 1}, colorMap: noTextureIndex}
	return m
}

func (m *BasicMaterial) record() *basicRecord { return (*basicRecord)(m.store.record(m.ref.id)) }

func (m *BasicMaterial) Color() glm.RGBA32F {
	return m.record().color
}
func (m *BasicMaterial) SetColor(color glm.RGBA32F) {
	m.record().color = color
}

func (m *BasicMaterial) Emissive() glm.RGBA32F {
	return m.record().emissive
}

func (m *BasicMaterial) SetEmissive(color glm.RGBA32F) {
	m.record().emissive = color
}

// ColorMap returns the bound color-map texture (or a zero handle).
func (m *BasicMaterial) ColorMap() Texture { return m.store.texture(m.ref.id, 0) }

// SetColorMap binds a base-color texture sampled with sampler (0 = default sampler).
func (m *BasicMaterial) SetColorMap(t Texture, sampler uint32) {
	m.store.setTexture(m.ref.id, 0, t)
	r := m.record()
	if t.Valid() {
		r.colorMap, r.colorSampler, r.flags = t.index, sampler, r.flags|MatColorMap
	} else {
		r.colorMap, r.flags = noTextureIndex, r.flags&^MatColorMap
	}
}

// ColorMapSampler is the bindless sampler index used for the color map.
func (m *BasicMaterial) ColorMapSampler() uint32 {
	return m.record().colorSampler
}

func (m *BasicMaterial) SetColorMapSampler(s uint32) {
	m.record().colorSampler = s
}

// Ref returns another generic Material handle to the same instance (for meshes).
func (m *BasicMaterial) Ref() Material { return m.genericMaterial.Copy() }
