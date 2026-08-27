package pix

import "github.com/bluescreen10/pix/glm"

// basicRecord is the GPU material record for unlit materials (scalar, 48 bytes);
// matches the Material struct in scene_unlit.frag. POD — uploaded verbatim.
type basicRecord struct {
	baseColor [4]float32
	emissive  [4]float32
	colorMap  uint32
	sampler   uint32
	flags     uint32
	_         uint32
}

// BasicMaterial is an unlit material: base color × vertex color × color map (+
// emissive), ignoring lights. It embeds genericMaterial (so it satisfies Material)
// and adds typed accessors over its own store.
type BasicMaterial struct {
	genericMaterial
	slab *materialSlab[basicRecord]
}

// NewBasicMaterial creates an unlit material (1 texture slot: the color map).
func (r *Renderer) NewBasicMaterial() *BasicMaterial {
	s := r.mats.basicStore()
	id := s.alloc(basicRecord{baseColor: [4]float32{1, 1, 1, 1}, colorMap: noTextureIndex})
	rc := int32(1)
	return &BasicMaterial{
		genericMaterial: genericMaterial{store: s, ref: Ref{id: id, gen: s.gens[id], refCount: &rc, owner: s}},
		slab:            s,
	}
}

func (m *BasicMaterial) rec() *basicRecord { return m.slab.get(m.ref.id) }

// Color returns the base color (rgb).
func (m *BasicMaterial) Color() glm.RGB32F {
	c := m.rec().baseColor
	return glm.RGB32F{c[0], c[1], c[2]}
}

// SetColor sets the base color (alpha stays 1).
func (m *BasicMaterial) SetColor(c glm.RGB32F) {
	m.rec().baseColor = [4]float32{c[0], c[1], c[2], 1}
	m.slab.touch()
}

// Emissive returns the emissive color.
func (m *BasicMaterial) Emissive() glm.RGB32F {
	e := m.rec().emissive
	return glm.RGB32F{e[0], e[1], e[2]}
}

// SetEmissive sets the emissive color (added on top of the albedo).
func (m *BasicMaterial) SetEmissive(c glm.RGB32F) {
	m.rec().emissive = [4]float32{c[0], c[1], c[2], 0}
	m.slab.touch()
}

// ColorMap returns the bound color-map texture (or a zero handle).
func (m *BasicMaterial) ColorMap() Texture { return m.slab.texture(m.ref.id, 0) }

// SetColorMap binds a base-color texture sampled with sampler (0 = default sampler);
// pass a zero Texture to clear it.
func (m *BasicMaterial) SetColorMap(t Texture, sampler uint32) {
	m.slab.setTexture(m.ref.id, 0, t)
	r := m.rec()
	if t.Valid() {
		r.colorMap, r.sampler, r.flags = t.index, sampler, r.flags|MatColorMap
	} else {
		r.colorMap, r.flags = noTextureIndex, r.flags&^MatColorMap
	}
}

// Ref returns another generic Material handle to the same instance (for meshes).
func (m *BasicMaterial) Ref() Material { return m.genericMaterial.Copy() }
