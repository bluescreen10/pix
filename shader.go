package pix

import "github.com/cogentcore/webgpu/wgpu"

type basicShader struct {
}

func (s *basicShader) Name() string {
	return "basic"
}

func (s *basicShader) VertexShader() string {
	return "shaderlib/basic.wgsl"
}

func (s *basicShader) FragmentShader() string {
	return "shaderlib/basic.wgsl"
}

func (s *basicShader) BindGroupLayout(device *wgpu.Device) (*wgpu.BindGroupLayout, error) {
	bindGroup, err := device.CreateBindGroupLayout(&wgpu.BindGroupLayoutDescriptor{
		Label:   "Basic material bind group",
		Entries: []wgpu.BindGroupLayoutEntry{},
	})

	return bindGroup, err
}

func (s *basicShader) CreateBindGroup(device *wgpu.Device, quque *wgpu.Queue) (*wgpu.BindGroup, error) {
	return nil, nil
}

type Shader interface {
	Name() string
	VertexShader() string
	FragmentShader() string
	BindGroupLayout(device *wgpu.Device) (*wgpu.BindGroupLayout, error)
	CreateBindGroup(device *wgpu.Device, quque *wgpu.Queue) (*wgpu.BindGroup, error)
}
