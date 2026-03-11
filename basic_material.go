package pix

import (
	"io"

	"github.com/bluescreen10/pix/glm"
)

var basicMaterialFragmentShader string
var basicMaterialVertexShader string

const (
	BasicMaterialColorMapFlag = uint64(1 << iota)
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
		m.flags |= BasicMaterialColorMapFlag
	} else {
		m.flags &^= BasicMaterialColorMapFlag
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
	if basicMaterialFragmentShader == "" {
		f, _ := shaderlib.Open("shaderlib/basic_material.fs")
		code, _ := io.ReadAll(f)
		basicMaterialFragmentShader = string(code)
	}

	if basicMaterialVertexShader == "" {
		f, _ := shaderlib.Open("shaderlib/basic_material.vs")
		code, _ := io.ReadAll(f)
		basicMaterialVertexShader = string(code)
	}

	uniform := (&Uniform{}).AddVec3("color").Build()

	data := NewMaterial(
		"Basic Material",
		basicMaterialVertexShader,
		basicMaterialFragmentShader,
		[]*Uniform{uniform},
		1,
	)

	builder := &BasicMaterial{
		MaterialData: data,
	}

	builder.SetColor(glm.Color3f{1, 1, 1})

	return builder
}
