package pix

import "github.com/cogentcore/webgpu/wgpu"

var matID idGen

type Material struct {
	id             uint32
	slot           int
	version        int
	vertexShader   string
	fragmentShader string

	textures []*TextureData
	uniforms []*Uniform
}

func (m *Material) Texture(id int) *TextureData {
	//FIXME: do bounds checking
	return m.textures[id]
}

func (m *Material) SetTexture(id int, texture *TextureData) {
	//FIXME: do bounds checking
	m.textures[id] = texture
	m.version++
}

func (m *Material) Uniforms() []*Uniform {
	return m.uniforms
}

func NewMaterial(vertexShader, fragmentShader string, uniforms []*Uniform, numTextures int) *Material {
	return &Material{
		id:             matID.Next(),
		vertexShader:   vertexShader,
		fragmentShader: fragmentShader,
		uniforms:       uniforms,
		textures:       make([]*TextureData, numTextures),
	}
}

type PreparedMaterial struct {
	version         int
	bindGroup       *wgpu.BindGroup
	bindGroupLayout *wgpu.BindGroupLayout
	fragmentShader  string
	vertexShader    string
	uniformBuffers  []*wgpu.Buffer
}
