package pix

import "github.com/bluescreen10/dawn-go/wgpu"

type TextureFormat = wgpu.TextureFormat

var textureID idGen

type TextureData struct {
	id      uint32
	version int
	width   int
	height  int
	format  TextureFormat
	sampler Sampler

	pendingData []byte

	// GPU-side resources, populated by the renderer.
	gpuVersion int
	gpuRef     *wgpu.Texture
	gpuView    *wgpu.TextureView
	gpuSampler *wgpu.Sampler
}

func (td *TextureData) AddressModeU() wgpu.AddressMode { return td.sampler.AddressModeU }
func (td *TextureData) SetAddressModeU(mode wgpu.AddressMode) {
	td.sampler.AddressModeU = mode
	td.version++
}

func (td *TextureData) AddressModeV() wgpu.AddressMode { return td.sampler.AddressModeV }
func (td *TextureData) SetAddressModeV(mode wgpu.AddressMode) {
	td.sampler.AddressModeV = mode
	td.version++
}

func (td *TextureData) AddressModeW() wgpu.AddressMode { return td.sampler.AddressModeW }
func (td *TextureData) SetAddressModeW(mode wgpu.AddressMode) {
	td.sampler.AddressModeW = mode
	td.version++
}

func (td *TextureData) MagFilter() wgpu.FilterMode { return td.sampler.MagFilter }
func (td *TextureData) SetMagFilter(mode wgpu.FilterMode) {
	td.sampler.MagFilter = mode
	td.version++
}

func (td *TextureData) MinFilter() wgpu.FilterMode { return td.sampler.MinFilter }
func (td *TextureData) SetMinFilter(mode wgpu.FilterMode) {
	td.sampler.MinFilter = mode
	td.version++
}

func (td *TextureData) MipmapFilter() wgpu.MipmapFilterMode { return td.sampler.MipmapFilter }
func (td *TextureData) SetMipmapFilter(mode wgpu.MipmapFilterMode) {
	td.sampler.MipmapFilter = mode
	td.version++
}

func (td *TextureData) LodMinClamp() float32     { return td.sampler.LodMinClamp }
func (td *TextureData) SetLodMinClamp(c float32) { td.sampler.LodMinClamp = c; td.version++ }

func (td *TextureData) LodMaxClamp() float32     { return td.sampler.LodMaxClamp }
func (td *TextureData) SetLodMaxClamp(c float32) { td.sampler.LodMaxClamp = c; td.version++ }

func (td *TextureData) Compare() wgpu.CompareFunction { return td.sampler.Compare }
func (td *TextureData) SetCompare(compare wgpu.CompareFunction) {
	td.sampler.Compare = compare
	td.version++
}

func (td *TextureData) MaxAnisotropy() uint16       { return td.sampler.MaxAnisotropy }
func (td *TextureData) SetMaxAnisotropy(max uint16) { td.sampler.MaxAnisotropy = max; td.version++ }

func (td *TextureData) hasPendingData() bool { return td.pendingData != nil }
func (td *TextureData) flush() []byte {
	data := td.pendingData
	td.pendingData = nil
	return data
}

// Destroy releases the GPU resources held by this texture.
func (td *TextureData) Destroy() {
	if td.gpuView != nil {
		td.gpuView.Release()
		td.gpuView = nil
	}
	if td.gpuRef != nil {
		td.gpuRef.Destroy()
		td.gpuRef = nil
	}
}

// Texture is the public handle for a renderer-owned texture resource.
type Texture struct {
	renderer *Renderer
	ref      Ref[Texture]
}

// Release surrenders this handle's reference to the texture resource.
func (t Texture) Release() { t.ref.Release() }

// Copy increments the reference count and returns an additional Texture handle.
func (t Texture) Copy() Texture { return Texture{renderer: t.renderer, ref: t.ref.Copy()} }

// Valid reports whether the underlying texture resource is still alive.
func (t Texture) Valid() bool { return t.ref.Valid() }
