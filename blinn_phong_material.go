package pix

import "github.com/bluescreen10/pix/glm"

// blinnPhongRecord is the GPU material record for Blinn-Phong materials (scalar, 48
// bytes); matches the Material struct in scene_lit.frag.
type blinnPhongRecord struct {
	baseColor [4]float32
	emissive  [4]float32
	specular  float32
	shininess float32
	colorMap  uint32
	sampler   uint32
	flags     uint32
	_, _, _   uint32
}

// BlinnPhongMaterial is ambient + diffuse (N·L) + Blinn-Phong specular.
type BlinnPhongMaterial struct {
	genericMaterial
	slab *materialSlab[blinnPhongRecord]
}

// NewBlinnPhongMaterial creates a Blinn-Phong material (1 texture slot: the color
// map). Defaults to white with a soft highlight.
func (r *Renderer) NewBlinnPhongMaterial() *BlinnPhongMaterial {
	s := r.mats.blinnStore()
	id := s.alloc(blinnPhongRecord{
		baseColor: [4]float32{1, 1, 1, 1}, specular: 0.3, shininess: 32, colorMap: noTextureIndex,
	})
	rc := int32(1)
	return &BlinnPhongMaterial{
		genericMaterial: genericMaterial{store: s, ref: Ref{id: id, gen: s.gens[id], refCount: &rc, owner: s}},
		slab:            s,
	}
}

func (m *BlinnPhongMaterial) rec() *blinnPhongRecord { return m.slab.get(m.ref.id) }

// Color returns the base color (rgba).
func (m *BlinnPhongMaterial) Color() glm.RGBA32F { return glm.RGBA32F(m.rec().baseColor) }

// SetColor sets the base color (rgba).
func (m *BlinnPhongMaterial) SetColor(c glm.RGBA32F) { m.rec().baseColor = c; m.slab.touch() }

// Specular returns the specular strength.
func (m *BlinnPhongMaterial) Specular() float32 { return m.rec().specular }

// SetSpecular sets the specular strength.
func (m *BlinnPhongMaterial) SetSpecular(v float32) { m.rec().specular = v; m.slab.touch() }

// Shininess returns the specular exponent.
func (m *BlinnPhongMaterial) Shininess() float32 { return m.rec().shininess }

// SetShininess sets the specular exponent.
func (m *BlinnPhongMaterial) SetShininess(v float32) { m.rec().shininess = v; m.slab.touch() }

// Emissive returns the emissive color.
func (m *BlinnPhongMaterial) Emissive() glm.RGB32F {
	e := m.rec().emissive
	return glm.RGB32F{e[0], e[1], e[2]}
}

// SetEmissive sets the emissive color.
func (m *BlinnPhongMaterial) SetEmissive(c glm.RGB32F) {
	m.rec().emissive = [4]float32{c[0], c[1], c[2], 0}
	m.slab.touch()
}

// ColorMap returns the bound color-map texture (or a zero handle).
func (m *BlinnPhongMaterial) ColorMap() Texture { return m.slab.texture(m.ref.id, 0) }

// SetColorMap binds a base-color texture sampled with sampler (0 = default sampler).
func (m *BlinnPhongMaterial) SetColorMap(t Texture, sampler uint32) {
	m.slab.setTexture(m.ref.id, 0, t)
	r := m.rec()
	if t.Valid() {
		r.colorMap, r.sampler, r.flags = t.index, sampler, r.flags|MatColorMap
	} else {
		r.colorMap, r.flags = noTextureIndex, r.flags&^MatColorMap
	}
}

// Ref returns another generic Material handle to the same instance (for meshes).
func (m *BlinnPhongMaterial) Ref() Material { return m.genericMaterial.Copy() }
