package pix

import "github.com/cogentcore/webgpu/wgpu"

var matID idGen

type MaterialData struct {
	id             uint32
	slot           int
	version        int
	vertexShader   string
	fragmentShader string

	textures []*TextureData
	uniforms []*Uniform
}

func (m *MaterialData) Texture(id int) *TextureData {
	//FIXME: do bounds checking
	return m.textures[id]
}

func (m *MaterialData) SetTexture(id int, texture *TextureData) {
	//FIXME: do bounds checking
	m.textures[id] = texture
	m.version++
}

func (m *MaterialData) Uniforms() []*Uniform {
	return m.uniforms
}

func NewMaterial(vertexShader, fragmentShader string, uniforms []*Uniform, numTextures int) *MaterialData {
	return &MaterialData{
		id:             matID.Next(),
		version:        1, // Force upload
		vertexShader:   vertexShader,
		fragmentShader: fragmentShader,
		uniforms:       uniforms,
		textures:       make([]*TextureData, numTextures),
	}
}

type Material struct {
	version         int
	bindGroup       *wgpu.BindGroup
	bindGroupLayout *wgpu.BindGroupLayout
	fragmentShader  string
	vertexShader    string
	uniformBuffers  []*wgpu.Buffer
}

func (m Material) Destroy() {
	if m.bindGroup != nil {
		m.bindGroup.Release()
	}

	if m.bindGroupLayout != nil {
		m.bindGroupLayout.Release()
	}

	for _, b := range m.uniformBuffers {
		b.Destroy()
	}
}
