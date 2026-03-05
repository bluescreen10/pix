package pix

import (
	"errors"
	"os"
	"unsafe"

	"github.com/bluescreen10/pix/glm"
	"github.com/cogentcore/webgpu/wgpu"
)

const (
	basicMaterialUniformSize = 16 // glm.Color3f
)

type basicMaterialShader struct {
	uniformBuffer *wgpu.Buffer

	bindGroupLayout *wgpu.BindGroupLayout
	bindGroup       *wgpu.BindGroup
}

func (s *basicMaterialShader) Name() string {
	return "basic"
}

func (s *basicMaterialShader) VertexShader() string {
	code, _ := os.ReadFile("shaderlib/basic_material.vs")
	return string(code)
}

func (s *basicMaterialShader) FragmentShader() string {
	code, _ := os.ReadFile("shaderlib/basic_material.fs")
	return string(code)
}

func (s *basicMaterialShader) BindGroupLayout(device *wgpu.Device) (*wgpu.BindGroupLayout, error) {
	layout, err := device.CreateBindGroupLayout(&wgpu.BindGroupLayoutDescriptor{
		Label: "Basic material bind group",
		Entries: []wgpu.BindGroupLayoutEntry{
			{
				Binding:    0, //TODO: Make it a named constant
				Visibility: wgpu.ShaderStageFragment,

				Buffer: wgpu.BufferBindingLayout{
					Type:             wgpu.BufferBindingTypeUniform,
					HasDynamicOffset: false,
					MinBindingSize:   uint64(unsafe.Sizeof(basicMaterialUniform{})),
				},
			},
			{
				Binding:    1, // Color map
				Visibility: wgpu.ShaderStageFragment,

				Texture: wgpu.TextureBindingLayout{
					Multisampled:  false,
					ViewDimension: wgpu.TextureViewDimension2D,
					SampleType:    wgpu.TextureSampleTypeFloat,
				},
			},
			{
				Binding:    2, // Color map sampler
				Visibility: wgpu.ShaderStageFragment,

				Sampler: wgpu.SamplerBindingLayout{
					Type: wgpu.SamplerBindingTypeFiltering,
				},
			},
		},
	})

	if err != nil {
		return nil, err
	}

	s.bindGroupLayout = layout
	return layout, nil
}

func (s *basicMaterialShader) Prepare(rawMaterial Material, device *wgpu.Device, resources *ResourceManager) error {
	material, ok := rawMaterial.(*BasicMaterial)
	if !ok {
		return errors.New("invalid material")
	}
	//TODO we can't have a global bindGroup it has to be one per instance
	if s.bindGroup != nil {
		return nil
	}

	if s.uniformBuffer == nil {
		buf, err := device.CreateBuffer(&wgpu.BufferDescriptor{
			Label: s.Name() + " uniform buffer",
			Size:  uint64(unsafe.Sizeof(basicMaterialUniform{})),
			Usage: wgpu.BufferUsageUniform | wgpu.BufferUsageCopyDst,
		})

		if err != nil {
			return err
		}

		s.uniformBuffer = buf
	}

	if s.bindGroupLayout == nil {
		_, err := s.BindGroupLayout(device)
		if err != nil {
			return err
		}
	}

	texture := resources.GetTexture(material.ColorMap())
	view := texture.gpuView

	sampler, ok := resources.samplers[texture.Sampler]
	if !ok {
		return errors.New("sampler not found")
	}

	bindGroup, err := device.CreateBindGroup(&wgpu.BindGroupDescriptor{
		Label:  s.Name() + " bind group",
		Layout: s.bindGroupLayout,
		Entries: []wgpu.BindGroupEntry{
			{
				Binding: 0, // Color
				Buffer:  s.uniformBuffer,
				Offset:  0,
				Size:    wgpu.WholeSize,
			},
			{
				Binding: 1, // Color map
				//TODO: pass in resources and get the texture that way
				TextureView: view,
			},
			{
				Binding: 2, // Color map sampler
				Sampler: sampler,
			},
		},
	})

	if err != nil {
		return err
	}

	s.bindGroup = bindGroup
	return nil
}

type basicMaterialUniform struct {
	color       glm.Color4f
	hasColorMap uint32
	_padding    [3]uint32 // Padding to align to 16 bytes (4 bytes + 12 bytes padding = 16)
}

func (s *basicMaterialShader) Bind(rawMaterial Material, pass *wgpu.RenderPassEncoder, queue *wgpu.Queue) error {
	material, ok := rawMaterial.(*BasicMaterial)
	if !ok {
		return errors.New("invalid material")
	}

	var hasColorMap uint32
	if material.ColorMap().IsValid() {
		hasColorMap = 1
	}

	err := queue.WriteBuffer(s.uniformBuffer, 0, wgpu.ToBytes([]basicMaterialUniform{{color: material.Color().RGBA(), hasColorMap: hasColorMap}}))
	if err != nil {
		return err
	}

	pass.SetBindGroup(1, s.bindGroup, []uint32{})
	return nil
}

type Shader interface {
	Name() string
	VertexShader() string
	FragmentShader() string
	BindGroupLayout(device *wgpu.Device) (*wgpu.BindGroupLayout, error)
	Prepare(material Material, device *wgpu.Device, resources *ResourceManager) error
	Bind(material Material, pass *wgpu.RenderPassEncoder, queue *wgpu.Queue) error
}
