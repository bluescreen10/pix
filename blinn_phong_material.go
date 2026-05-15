package pix

import (
	"github.com/bluescreen10/pix/glm"
)

type BlinnPhongMaterial struct {
	Material
}

func (m *BlinnPhongMaterial) data() *MaterialData {
	return m.renderer.materials.get(m.ref.ID())
}

func (m *BlinnPhongMaterial) SetColor(color glm.Color3f) {
	data := m.data()
	data.uniforms[0].SetVec3("color", glm.Vec3f{color[0], color[1], color[2]})
	data.version++
}

func (m *BlinnPhongMaterial) Color() glm.Color3f {
	v := m.data().uniforms[0].Vec3("color")
	return glm.Color3f{v[0], v[1], v[2]}
}

func (m *BlinnPhongMaterial) SetColorMap(texture Texture) {
	data := m.data()
	old := data.textures[0]
	data.textures[0] = texture.ref.Copy()
	old.Release()
	if texture.ref.Valid() {
		data.flags |= ColorMapFlag
	} else {
		data.flags &^= ColorMapFlag
	}
	data.version++
}

func (m *BlinnPhongMaterial) ColorMap() Ref[Texture] {
	return m.data().textures[0]
}

func (m *BlinnPhongMaterial) NeedsUpdate() {
	m.data().version++
}

// Ref returns a new Material handle sharing the same underlying resource.
func (m *BlinnPhongMaterial) Ref() Material {
	return m.Material.Copy()
}

// Release surrenders the builder's own reference to the material resource.
func (m *BlinnPhongMaterial) Release() {
	m.Material.Release()
	m.Material = Material{}
}

// NewBlinnPhongMaterial creates a Blinn-Phong lit material owned by the renderer.
func (r *Renderer) NewBlinnPhongMaterial() *BlinnPhongMaterial {
	uniform := (&Uniform{}).AddVec3("color").Build()
	data := NewMaterial("Blinn-Phong Material", "vertex.wesl", "blinn_phong_material.wesl", []*Uniform{uniform}, 1, true)
	mat := r.allocMaterialSlot(data)
	bm := &BlinnPhongMaterial{Material: mat}
	bm.SetColor(glm.Color3f{1, 1, 1})
	return bm
}
