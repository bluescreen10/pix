package pix

import (
	"errors"
	"os"

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
	code, _ := os.ReadFile("shaderlib/basic.wgsl")
	return string(code)
}

func (s *basicMaterialShader) FragmentShader() string {
	code, _ := os.ReadFile("shaderlib/basic.wgsl")
	return string(code)
}

func (s *basicMaterialShader) BindGroupLayout(device *wgpu.Device) (*wgpu.BindGroupLayout, error) {
	layout, err := device.CreateBindGroupLayout(&wgpu.BindGroupLayoutDescriptor{
		Label: "Basic material bind group",
		Entries: []wgpu.BindGroupLayoutEntry{
			{
				Binding:    0, //TODO: Make it a constant
				Visibility: wgpu.ShaderStageFragment,

				Buffer: wgpu.BufferBindingLayout{
					Type:             wgpu.BufferBindingTypeUniform,
					HasDynamicOffset: false,
					MinBindingSize:   basicMaterialUniformSize,
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

func (s *basicMaterialShader) Prepare(rawMaterial Material, device *wgpu.Device, queue *wgpu.Queue) error {
	//TODO we can't have a global bindGroup it has to be one per instance
	if s.bindGroup != nil {
		return nil
	}

	if s.uniformBuffer == nil {
		buf, err := device.CreateBuffer(&wgpu.BufferDescriptor{
			Label: s.Name() + " uniform buffer",
			Size:  basicMaterialUniformSize,
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
		},
	})

	if err != nil {
		return err
	}

	s.bindGroup = bindGroup
	return nil
}

func (s *basicMaterialShader) Bind(rawMaterial Material, pass *wgpu.RenderPassEncoder, queue *wgpu.Queue) error {
	material, ok := rawMaterial.(*BasicMaterial)
	if !ok {
		return errors.New("invalid material")
	}
	err := queue.WriteBuffer(s.uniformBuffer, 0, wgpu.ToBytes([]glm.Color3f{material.Color()}))
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
	Prepare(material Material, device *wgpu.Device, queue *wgpu.Queue) error
	Bind(material Material, pass *wgpu.RenderPassEncoder, queue *wgpu.Queue) error
}
