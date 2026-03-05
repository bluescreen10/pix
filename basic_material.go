package pix

import "github.com/bluescreen10/pix/glm"

var _ Material = &BasicMaterial{}

type BasicMaterial struct {
	version  int
	color    glm.Color3f
	colorMap Handle[Texture]
}

func (m *BasicMaterial) SetColor(color glm.Color3f) {
	m.color = color
	m.version++
}

func (m *BasicMaterial) Color() glm.Color3f {
	return m.color
}

func (m *BasicMaterial) SetColorMap(texture Handle[Texture]) {
	m.colorMap = texture
	m.version++
}

func (m *BasicMaterial) ColorMap() Handle[Texture] {
	return m.colorMap
}

func (m *BasicMaterial) Shader() string {
	return "basic"
}
