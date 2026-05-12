package pix

import (
	"github.com/bluescreen10/pix/glm"
)

type BasicMaterial struct {
	*MaterialData
}

func (m *BasicMaterial) SetColor(color glm.Color3f) {
	m.uniforms[0].SetVec3("color", glm.Vec3f{color[0], color[1], color[2]})
	m.version++
}

func (m *BasicMaterial) Color() glm.Color3f {
	v := m.uniforms[0].Vec3("color")
	return glm.Color3f{v[0], v[1], v[2]}
}

func (m *BasicMaterial) SetColorMap(texture *TextureData) {

	// adjust flags
	if texture != nil {
		m.flags |= ColorMapFlag
	} else {
		m.flags &^= ColorMapFlag
	}

	m.textures[0] = texture
	m.NeedsUpdate()
}

func (m *BasicMaterial) ColorMap() *TextureData {
	return m.textures[0]
}

func (m *BasicMaterial) NeedsUpdate() {
	m.version++
}

func (m *BasicMaterial) Build() *MaterialData {
	return m.MaterialData
}

func NewBasicMaterial() *BasicMaterial {
	uniform := (&Uniform{}).AddVec3("color").Build()

	data := NewMaterial(
		"Basic Material",
		//basicMaterialshader,
		"basic_material.wesl",
		[]*Uniform{uniform},
		1,
		false,
	)

	builder := &BasicMaterial{
		MaterialData: data,
	}

	builder.SetColor(glm.Color3f{1, 1, 1})

	return builder
}
