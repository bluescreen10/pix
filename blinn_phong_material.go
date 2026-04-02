package pix

import (
	"io"

	"github.com/bluescreen10/pix/glm"
)

var blinnPhongMaterialFragmentShader string
var blinnPhongMaterialVertexShader string

type BlinnPhongMaterial struct {
	*MaterialData
}

func (m *BlinnPhongMaterial) SetColor(color glm.Color3f) {
	m.uniforms[0].SetVec3("color", glm.Vec3f{color[0], color[1], color[2]})
	m.version++
}

func (m *BlinnPhongMaterial) Color() glm.Color3f {
	v := m.uniforms[0].Vec3("color")
	return glm.Color3f{v[0], v[1], v[2]}
}

func (m *BlinnPhongMaterial) SetColorMap(texture *TextureData) {

	// adjust flags
	if texture != nil {
		m.flags |= ColorMapFlag
	} else {
		m.flags &^= ColorMapFlag
	}

	m.textures[0] = texture
	m.NeedsUpdate()
}

func (m *BlinnPhongMaterial) ColorMap() *TextureData {
	return m.textures[0]
}

func (m *BlinnPhongMaterial) NeedsUpdate() {
	m.version++
}

func (m *BlinnPhongMaterial) Build() *MaterialData {
	return m.MaterialData
}

func NewBlinnPhongMaterial() *BlinnPhongMaterial {
	if blinnPhongMaterialFragmentShader == "" {
		f, _ := shaderlib.Open("shaderlib/blinn_phong_material.fs")
		code, _ := io.ReadAll(f)
		blinnPhongMaterialFragmentShader = string(code)
	}

	if blinnPhongMaterialVertexShader == "" {
		f, _ := shaderlib.Open("shaderlib/blinn_phong_material.vs")
		code, _ := io.ReadAll(f)
		blinnPhongMaterialVertexShader = string(code)
	}

	uniform := (&Uniform{}).AddVec3("color").Build()

	data := NewMaterial(
		"Basic Material",
		blinnPhongMaterialVertexShader,
		blinnPhongMaterialFragmentShader,
		[]*Uniform{uniform},
		1,
		true,
	)

	builder := &BlinnPhongMaterial{
		MaterialData: data,
	}

	builder.SetColor(glm.Color3f{1, 1, 1})

	return builder
}
