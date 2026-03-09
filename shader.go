package pix

import (
	"embed"
	"errors"
	"io"
	"unsafe"

	"github.com/bluescreen10/pix/glm"
	"github.com/cogentcore/webgpu/wgpu"
)

//go:embed shaderlib/*
var shaderlib embed.FS

var ErrResourceNotReady = errors.New("resources not ready")

type basicMaterialShader struct {
	uniformBuffer *wgpu.Buffer

	bindGroupLayout *wgpu.BindGroupLayout
	bindGroup       *wgpu.BindGroup
	hash            resourceHash
}

func (s *basicMaterialShader) Name() string {
	return "basic"
}

func (s *basicMaterialShader) VertexShader() string {
	f, _ := shaderlib.Open("shaderlib/basic_material.vs")
	code, _ := io.ReadAll(f)
	return string(code)
}

func (s *basicMaterialShader) FragmentShader() string {
	f, _ := shaderlib.Open("shaderlib/basic_material.fs")
	code, _ := io.ReadAll(f)
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

func (s *basicMaterialShader) Prepare(rawData Material, device *wgpu.Device, resources *resourceManager) (PreparedMaterial, error) {
	data, ok := rawData.(*BasicMaterial)
	if !ok {
		return PreparedMaterial{}, errors.New("invalid material")
	}
	//TODO we can't have a global bindGroup it has to be one per instance

	material := resources.GetBasicMaterialByData(data)

	if data.version == material.version && material.bindGroup != nil {
		return material, nil
	}

	if material.bindGroup != nil {
		material.bindGroup.Release()
		material.bindGroup = nil
	}

	if s.uniformBuffer == nil {
		buf, err := device.CreateBuffer(&wgpu.BufferDescriptor{
			Label: s.Name() + " uniform buffer",
			Size:  uint64(unsafe.Sizeof(basicMaterialUniform{})),
			Usage: wgpu.BufferUsageUniform | wgpu.BufferUsageCopyDst,
		})

		if err != nil {
			return PreparedMaterial{}, err
		}

		s.uniformBuffer = buf
	}

	if s.bindGroupLayout == nil {
		_, err := s.BindGroupLayout(device)
		if err != nil {
			return PreparedMaterial{}, err
		}
	}

	var view *wgpu.TextureView
	var sampler *wgpu.Sampler

	if colorMap := data.ColorMap(); colorMap != nil {
		texture := resources.GetTextureByData(data.ColorMap())

		// if texture is not ready, upload it
		if texture.view == nil || texture.version != colorMap.version {
			if err := resources.uploadTexture(colorMap, device); err != nil {
				return PreparedMaterial{}, err
			}

			texture = resources.GetTextureByData(data.ColorMap())
		}
		view = texture.view
		sampler = texture.sampler

	}

	queue := device.GetQueue()

	if view == nil {
		panic(view)
	}

	var hasColorMap uint32
	if view != nil {
		hasColorMap = 1
	}

	err := queue.WriteBuffer(s.uniformBuffer, 0, wgpu.ToBytes([]basicMaterialUniform{{color: data.Color().RGBA(), hasColorMap: hasColorMap}}))
	if err != nil {
		return PreparedMaterial{}, err
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
		return PreparedMaterial{}, err
	}

	material.bindGroup = bindGroup
	material.version = data.version
	material.shader = s
	resources.SetBasicMaterialResource(data.id, material)
	return material, nil
}

type basicMaterialUniform struct {
	color       glm.Color4f
	hasColorMap uint32
	_padding    [3]uint32 // Padding to align to 16 bytes (4 bytes + 12 bytes padding = 16)
}

type Shader interface {
	Name() string
	VertexShader() string
	FragmentShader() string
	BindGroupLayout(device *wgpu.Device) (*wgpu.BindGroupLayout, error)
	Prepare(material Material, device *wgpu.Device, resources *resourceManager) (PreparedMaterial, error)
}
