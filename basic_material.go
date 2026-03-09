package pix

import (
	"github.com/bluescreen10/pix/glm"
)

var _ Material = &BasicMaterial{}

type resourceHash uint64

func (h resourceHash) Combine(v uint64) resourceHash {
	const magic resourceHash = 0x9e3779b97f4a7c15
	return h ^ (resourceHash(v) + magic + (h << 6) + (h >> 2))
}

type BasicMaterial struct {
	id       int
	version  int
	color    glm.Color3f
	colorMap *TextureData
}

func (m *BasicMaterial) SetColor(color glm.Color3f) {
	m.color = color
	m.NeedsUpdate()
}

func (m *BasicMaterial) Color() glm.Color3f {
	return m.color
}

func (m *BasicMaterial) SetColorMap(texture *TextureData) {
	m.colorMap = texture
	m.NeedsUpdate()
}

func (m *BasicMaterial) ColorMap() *TextureData {
	return m.colorMap
}

func (m *BasicMaterial) Shader() string {
	return "basic"
}

func (m *BasicMaterial) NeedsUpdate() {
	m.version++
}

func (m *BasicMaterial) Version() int {
	return m.version
}

func NewBasicMaterial() *BasicMaterial {
	return &BasicMaterial{
		color: glm.Color3f{1, 1, 1},
	}
}
