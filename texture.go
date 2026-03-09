package pix

import "github.com/cogentcore/webgpu/wgpu"

type TextureFormat = wgpu.TextureFormat

type TextureData struct {
	id          int
	version     int
	width       int
	height      int
	format      TextureFormat
	sampler     Sampler
	pendingData []byte
}

func (td *TextureData) AddressModeU() wgpu.AddressMode {
	return td.sampler.AddressModeU
}

func (td *TextureData) SetAddressModeU(mode wgpu.AddressMode) {
	td.sampler.AddressModeU = mode
	td.version++
}

func (td *TextureData) AddressModeV() wgpu.AddressMode {
	return td.sampler.AddressModeV
}

func (td *TextureData) SetAddressModeV(mode wgpu.AddressMode) {
	td.sampler.AddressModeV = mode
	td.version++
}

func (td *TextureData) AddressModeW() wgpu.AddressMode {
	return td.sampler.AddressModeW
}

func (td *TextureData) SetAddressModeW(mode wgpu.AddressMode) {
	td.sampler.AddressModeW = mode
	td.version++
}

func (td *TextureData) MagFilter() wgpu.FilterMode {
	return td.sampler.MagFilter
}

func (td *TextureData) SetMagFilter(mode wgpu.FilterMode) {
	td.sampler.MagFilter = mode
	td.version++
}

func (td *TextureData) MinFilter() wgpu.FilterMode {
	return td.sampler.MinFilter
}

func (td *TextureData) SetMinFilter(mode wgpu.FilterMode) {
	td.sampler.MinFilter = mode
	td.version++
}

func (td *TextureData) MipmapFilter() wgpu.MipmapFilterMode {
	return td.sampler.MipmapFilter
}

func (td *TextureData) SetMipmapFilter(mode wgpu.MipmapFilterMode) {
	td.sampler.MipmapFilter = mode
	td.version++
}

func (td *TextureData) LodMinClamp() float32 {
	return td.sampler.LodMinClamp
}

func (td *TextureData) SetLodMinClamp(clamp float32) {
	td.sampler.LodMinClamp = clamp
	td.version++
}

func (td *TextureData) LodMaxClamp() float32 {
	return td.sampler.LodMaxClamp
}

func (td *TextureData) SetLodMaxClamp(clamp float32) {
	td.sampler.LodMaxClamp = clamp
	td.version++
}

func (td *TextureData) Compare() wgpu.CompareFunction {
	return td.sampler.Compare
}

func (td *TextureData) SetCompare(compare wgpu.CompareFunction) {
	td.sampler.Compare = compare
	td.version++
}

func (td *TextureData) MaxAnisotropy() uint16 {
	return td.sampler.MaxAnisotropy
}

func (td *TextureData) SetMaxAnisotropy(max uint16) {
	td.sampler.MaxAnisotropy = max
	td.version++
}

func (td *TextureData) Version() int {
	return td.version
}

func (td *TextureData) hasPendingData() bool {
	return td.pendingData != nil
}

func (td *TextureData) flush() []byte {
	data := td.pendingData
	td.pendingData = nil
	return data
}

type Texture struct {
	version int
	ref     *wgpu.Texture
	view    *wgpu.TextureView
	sampler *wgpu.Sampler
}

func (t *Texture) Destroy() {
	if t.view != nil {
		t.view.Release()
	}

	if t.ref != nil {
		t.ref.Destroy()
	}
}

func (t *Texture) Version() int {
	return t.version
}
