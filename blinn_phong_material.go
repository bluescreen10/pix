package pix

import (
	"unsafe"

	"github.com/bluescreen10/pix/glm"
)

// blinnPhongRecord is the GPU material record for Blinn-Phong (64 bytes); matches the
// Material struct in scene_lit.frag.
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
}

// NewBlinnPhongMaterial creates a Blinn-Phong material (1 texture slot: the color
// map). Defaults to white with a soft highlight.
func (r *Renderer) NewBlinnPhongMaterial() *BlinnPhongMaterial {
	st := r.mats.store(ShaderBlinnPhong, uint32(unsafe.Sizeof(blinnPhongRecord{})), 1, "BlinnPhong Materials")
	id := st.alloc()
	rc := int32(1)
	m := &BlinnPhongMaterial{genericMaterial{store: st, ref: Ref{id: id, gen: st.gens[id], refCount: &rc, owner: st}}}
	*m.rec() = blinnPhongRecord{baseColor: [4]float32{1, 1, 1, 1}, specular: 0.3, shininess: 32, colorMap: noTextureIndex}
	return m
}

func (m *BlinnPhongMaterial) rec() *blinnPhongRecord { return (*blinnPhongRecord)(m.store.record(m.ref.id)) }

func (m *BlinnPhongMaterial) Color() glm.RGBA32F     { return glm.RGBA32F(m.rec().baseColor) }
func (m *BlinnPhongMaterial) SetColor(c glm.RGBA32F) { m.rec().baseColor = c }

func (m *BlinnPhongMaterial) Specular() float32     { return m.rec().specular }
func (m *BlinnPhongMaterial) SetSpecular(v float32) { m.rec().specular = v }

func (m *BlinnPhongMaterial) Shininess() float32     { return m.rec().shininess }
func (m *BlinnPhongMaterial) SetShininess(v float32) { m.rec().shininess = v }

func (m *BlinnPhongMaterial) Emissive() glm.RGB32F {
	e := m.rec().emissive
	return glm.RGB32F{e[0], e[1], e[2]}
}
func (m *BlinnPhongMaterial) SetEmissive(c glm.RGB32F) { m.rec().emissive = [4]float32{c[0], c[1], c[2], 0} }

// ColorMap returns the bound color-map texture (or a zero handle).
func (m *BlinnPhongMaterial) ColorMap() Texture { return m.store.texture(m.ref.id, 0) }

// SetColorMap binds a base-color texture sampled with sampler (0 = default sampler).
func (m *BlinnPhongMaterial) SetColorMap(t Texture, sampler uint32) {
	m.store.setTexture(m.ref.id, 0, t)
	r := m.rec()
	if t.Valid() {
		r.colorMap, r.sampler, r.flags = t.index, sampler, r.flags|MatColorMap
	} else {
		r.colorMap, r.flags = noTextureIndex, r.flags&^MatColorMap
	}
}

// Ref returns another generic Material handle to the same instance (for meshes).
func (m *BlinnPhongMaterial) Ref() Material { return m.genericMaterial.Copy() }
