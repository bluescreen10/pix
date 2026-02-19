package pix

import "github.com/bluescreen10/pix/glm"

var _ Material = &BasicMaterial{}

type BasicMaterial struct {
	version int
	color   glm.Color3f
}

func (m *BasicMaterial) SetColor(color glm.Color3f) {
	m.color = color
	m.version++
}

func (m *BasicMaterial) Color() glm.Color3f {
	return m.color
}

func (m *BasicMaterial) Shader() string {
	return "basic"
}
