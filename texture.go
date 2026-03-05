package pix

import "github.com/cogentcore/webgpu/wgpu"

type TextureFormat = wgpu.TextureFormat

type Texture struct {
	Width       int
	Height      int
	Format      TextureFormat
	Sampler     Sampler
	PendingData []byte

	// gpu resources
	gpuTexture *wgpu.Texture
	gpuView    *wgpu.TextureView
}

func (t *Texture) AddressModeU() wgpu.AddressMode {
	return t.Sampler.AddressModeU
}

func (t *Texture) SetAddressModeU(mode wgpu.AddressMode) {
	t.Sampler.AddressModeU = mode
}

func (t *Texture) AddressModeV() wgpu.AddressMode {
	return t.Sampler.AddressModeV
}

func (t *Texture) SetAddressModeV(mode wgpu.AddressMode) {
	t.Sampler.AddressModeV = mode
}

func (t *Texture) AddressModeW() wgpu.AddressMode {
	return t.Sampler.AddressModeW
}

func (t *Texture) SetAddressModeW(mode wgpu.AddressMode) {
	t.Sampler.AddressModeW = mode
}

func (t *Texture) MagFilter() wgpu.FilterMode {
	return t.Sampler.MagFilter
}

func (t *Texture) SetMagFilter(mode wgpu.FilterMode) {
	t.Sampler.MagFilter = mode
}

func (t *Texture) MinFilter() wgpu.FilterMode {
	return t.Sampler.MinFilter
}

func (t *Texture) SetMinFilter(mode wgpu.FilterMode) {
	t.Sampler.MinFilter = mode
}

func (t *Texture) MipmapFilter() wgpu.MipmapFilterMode {
	return t.Sampler.MipmapFilter
}

func (t *Texture) SetMipmapFilter(mode wgpu.MipmapFilterMode) {
	t.Sampler.MipmapFilter = mode
}

func (t *Texture) LodMinClamp() float32 {
	return t.Sampler.LodMinClamp
}

func (t *Texture) SetLodMinClamp(clamp float32) {
	t.Sampler.LodMinClamp = clamp
}

func (t *Texture) LodMaxClamp() float32 {
	return t.Sampler.LodMaxClamp
}

func (t *Texture) SetLodMaxClamp(clamp float32) {
	t.Sampler.LodMaxClamp = clamp
}

func (t *Texture) Compare() wgpu.CompareFunction {
	return t.Sampler.Compare
}

func (t *Texture) SetCompare(compare wgpu.CompareFunction) {
	t.Sampler.Compare = compare
}

func (t *Texture) MaxAnisotropy() uint16 {
	return t.Sampler.MaxAnisotropy
}

func (t *Texture) SetMaxAnisotropy(max uint16) {
	t.Sampler.MaxAnisotropy = max
}

func (t *Texture) View() *wgpu.TextureView {
	return t.gpuView
}
