package pix

import "github.com/cogentcore/webgpu/wgpu"

// TODO: reduce the size of the struct to fit in a register
type Sampler struct {
	AddressModeU  wgpu.AddressMode
	AddressModeV  wgpu.AddressMode
	AddressModeW  wgpu.AddressMode
	MagFilter     wgpu.FilterMode
	MinFilter     wgpu.FilterMode
	MipmapFilter  wgpu.MipmapFilterMode
	LodMinClamp   float32
	LodMaxClamp   float32
	Compare       wgpu.CompareFunction
	MaxAnisotropy uint16
}
