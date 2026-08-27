package pix

import (
	"unsafe"

	"github.com/bluescreen10/pix/glm"
)

// basicRecord is the GPU material record for unlit materials (48 bytes); matches the
// Material struct in scene_unlit.frag.
type basicRecord struct {
	baseColor [4]float32
	emissive  [4]float32
	colorMap  uint32
	sampler   uint32
	flags     uint32
	_         uint32
}

// BasicMaterial is an unlit material: base color × vertex color × color map (+
// emissive), ignoring lights.
type BasicMaterial struct {
	genericMaterial
}

// NewBasicMaterial creates an unlit material (1 texture slot: the color map).
func (r *Renderer) NewBasicMaterial() *BasicMaterial {
	st := r.mats.store(ShaderUnlit, uint32(unsafe.Sizeof(basicRecord{})), 1, "Basic Materials")
	id := st.alloc()
	rc := int32(1)
	m := &BasicMaterial{genericMaterial{store: st, ref: Ref{id: id, gen: st.gens[id], refCount: &rc, owner: st}}}
	*m.rec() = basicRecord{baseColor: [4]float32{1, 1, 1, 1}, colorMap: noTextureIndex}
	return m
}

func (m *BasicMaterial) rec() *basicRecord { return (*basicRecord)(m.store.record(m.ref.id)) }

func (m *BasicMaterial) Color() glm.RGB32F {
	c := m.rec().baseColor
	return glm.RGB32F{c[0], c[1], c[2]}
}
func (m *BasicMaterial) SetColor(c glm.RGB32F) { m.rec().baseColor = [4]float32{c[0], c[1], c[2], 1} }

func (m *BasicMaterial) Emissive() glm.RGB32F {
	e := m.rec().emissive
	return glm.RGB32F{e[0], e[1], e[2]}
}
func (m *BasicMaterial) SetEmissive(c glm.RGB32F) { m.rec().emissive = [4]float32{c[0], c[1], c[2], 0} }

// ColorMap returns the bound color-map texture (or a zero handle).
func (m *BasicMaterial) ColorMap() Texture { return m.store.texture(m.ref.id, 0) }

// SetColorMap binds a base-color texture sampled with sampler (0 = default sampler).
func (m *BasicMaterial) SetColorMap(t Texture, sampler uint32) {
	m.store.setTexture(m.ref.id, 0, t)
	r := m.rec()
	if t.Valid() {
		r.colorMap, r.sampler, r.flags = t.index, sampler, r.flags|MatColorMap
	} else {
		r.colorMap, r.flags = noTextureIndex, r.flags&^MatColorMap
	}
}

// Ref returns another generic Material handle to the same instance (for meshes).
func (m *BasicMaterial) Ref() Material { return m.genericMaterial.Copy() }
