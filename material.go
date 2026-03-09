package pix

import "github.com/cogentcore/webgpu/wgpu"

type Material interface {
	Shader() string
}

type PreparedMaterial struct {
	version   int
	bindGroup *wgpu.BindGroup
	shader    Shader
}
