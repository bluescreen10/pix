package pix

import (
	"hash/fnv"
	"unsafe"

	"github.com/bluescreen10/dawn-go/wgpu"
)

var matID idGen

type MaterialFlags uint32

const (
	ColorMapFlag = MaterialFlags(1 << iota)
)

var materialFlagNames = map[int]string{
	0: "USE_MAP",
}

type MaterialData struct {
	id             uint32
	version        int
	vertexShader   string
	fragmentShader string
	name           string
	hash           uint32
	flags          MaterialFlags
	side           Side
	blending       BlendMode
	depthWrite     bool
	depthTest      bool
	depthFunc      DepthFunc
	colorWrite     bool
	textures       []Ref[Texture]
	uniforms       []*Uniform
	isLit          bool

	// GPU-side resources, populated by the renderer.
	gpuVersion         int
	gpuBindGroup       *wgpu.BindGroup
	gpuBindGroupLayout *wgpu.BindGroupLayout
	gpuUniformBuffers  []*wgpu.Buffer
}

func (m *MaterialData) Texture(id int) Ref[Texture] {
	return m.textures[id]
}

func (m *MaterialData) SetTexture(id int, texture Texture) {
	old := m.textures[id]
	m.textures[id] = texture.ref.Copy()
	old.Release()
	m.version++
}

func (m *MaterialData) Uniforms() []*Uniform {
	return m.uniforms
}

// Destroy releases the GPU resources held by this material.
func (m *MaterialData) Destroy() {
	if m.gpuBindGroup != nil {
		m.gpuBindGroup.Release()
		m.gpuBindGroup = nil
	}
	if m.gpuBindGroupLayout != nil {
		m.gpuBindGroupLayout.Release()
		m.gpuBindGroupLayout = nil
	}
	for _, b := range m.gpuUniformBuffers {
		b.Destroy()
	}
	m.gpuUniformBuffers = nil
}

func NewMaterial(name string, vertexShader string, fragmentShader string, uniforms []*Uniform, numTextures int, isLit bool) *MaterialData {
	return &MaterialData{
		id:             matID.Next(),
		name:           name,
		version:        1,
		vertexShader:   vertexShader,
		fragmentShader: fragmentShader,
		hash:           hashShader(vertexShader, fragmentShader),
		side:           SideFront,
		blending:       BlendOpaque,
		depthWrite:     true,
		depthTest:      true,
		depthFunc:      DepthFuncLess,
		colorWrite:     true,
		uniforms:       uniforms,
		textures:       make([]Ref[Texture], numTextures),
		isLit:          isLit,
	}
}

// Material is the public handle for a renderer-owned material resource.
type Material struct {
	renderer *Renderer
	ref      Ref[Material]
}

func (m Material) Ref() Ref[Material] {
	return m.ref
}

// Release surrenders this handle's reference to the material resource.
func (m Material) Release() { m.ref.Release() }

// Copy increments the reference count and returns an additional Material handle.
func (m Material) Copy() Material { return Material{renderer: m.renderer, ref: m.ref.Copy()} }

// Valid reports whether the underlying material resource is still alive.
func (m Material) Valid() bool { return m.ref.Valid() }

func hashShader(s1, s2 string) uint32 {
	h := fnv.New32a()
	h.Write(unsafe.Slice(unsafe.StringData(s1), len(s1)))
	h.Write(unsafe.Slice(unsafe.StringData(s2), len(s2)))
	return h.Sum32()
}
