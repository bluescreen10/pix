package pix

import (
	"github.com/bluescreen10/pix/glm"
)

type BasicMaterial struct {
	Material
}

func (m *BasicMaterial) data() *MaterialData {
	return m.renderer.materials.get(m.ref.ID())
}

func (m *BasicMaterial) SetColor(color glm.Color3f) {
	data := m.data()
	data.uniforms[0].SetVec3("color", glm.Vec3f{color[0], color[1], color[2]})
	data.version++
}

func (m *BasicMaterial) Color() glm.Color3f {
	v := m.data().uniforms[0].Vec3("color")
	return glm.Color3f{v[0], v[1], v[2]}
}

func (m *BasicMaterial) SetColorMap(texture Texture) {
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

func (m *BasicMaterial) ColorMap() Ref[Texture] {
	return m.data().textures[0]
}

func (m *BasicMaterial) NeedsUpdate() {
	m.data().version++
}

// Ref returns a new Material handle sharing the same underlying resource.
func (m *BasicMaterial) Ref() Material {
	return m.Material.Copy()
}

// Release surrenders the builder's own reference to the material resource.
func (m *BasicMaterial) Release() {
	m.Material.Release()
	m.Material = Material{}
}

// NewBasicMaterial creates a basic (unlit) material owned by the renderer.
func (r *Renderer) NewBasicMaterial() *BasicMaterial {
	uniform := (&Uniform{}).AddVec3("color").Build()
	data := NewMaterial("Basic Material", "vertex.wesl", "basic_material.wesl", []*Uniform{uniform}, 1, false)
	mat := r.allocMaterialSlot(data)
	bm := &BasicMaterial{Material: mat}
	bm.SetColor(glm.Color3f{1, 1, 1})
	return bm
}
