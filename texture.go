package pix

import "github.com/bluescreen10/dawn-go/wgpu"

type TextureFormat uint32

const (
	TextureFormatUndefined            TextureFormat = 0
	TextureFormatR8Unorm              TextureFormat = 1
	TextureFormatR8Snorm              TextureFormat = 2
	TextureFormatR8Uint               TextureFormat = 3
	TextureFormatR8Sint               TextureFormat = 4
	TextureFormatR16Unorm             TextureFormat = 5
	TextureFormatR16Snorm             TextureFormat = 6
	TextureFormatR16Uint              TextureFormat = 7
	TextureFormatR16Sint              TextureFormat = 8
	TextureFormatR16Float             TextureFormat = 9
	TextureFormatRG8Unorm             TextureFormat = 10
	TextureFormatRG8Snorm             TextureFormat = 11
	TextureFormatRG8Uint              TextureFormat = 12
	TextureFormatRG8Sint              TextureFormat = 13
	TextureFormatR32Float             TextureFormat = 14
	TextureFormatR32Uint              TextureFormat = 15
	TextureFormatR32Sint              TextureFormat = 16
	TextureFormatRG16Unorm            TextureFormat = 17
	TextureFormatRG16Snorm            TextureFormat = 18
	TextureFormatRG16Uint             TextureFormat = 19
	TextureFormatRG16Sint             TextureFormat = 20
	TextureFormatRG16Float            TextureFormat = 21
	TextureFormatRGBA8Unorm           TextureFormat = 22
	TextureFormatRGBA8UnormSRGB       TextureFormat = 23
	TextureFormatRGBA8Snorm           TextureFormat = 24
	TextureFormatRGBA8Uint            TextureFormat = 25
	TextureFormatRGBA8Sint            TextureFormat = 26
	TextureFormatBGRA8Unorm           TextureFormat = 27
	TextureFormatBGRA8UnormSRGB       TextureFormat = 28
	TextureFormatRGB10A2Uint          TextureFormat = 29
	TextureFormatRGB10A2Unorm         TextureFormat = 30
	TextureFormatRG11B10Ufloat        TextureFormat = 31
	TextureFormatRGB9E5Ufloat         TextureFormat = 32
	TextureFormatRG32Float            TextureFormat = 33
	TextureFormatRG32Uint             TextureFormat = 34
	TextureFormatRG32Sint             TextureFormat = 35
	TextureFormatRGBA16Unorm          TextureFormat = 36
	TextureFormatRGBA16Snorm          TextureFormat = 37
	TextureFormatRGBA16Uint           TextureFormat = 38
	TextureFormatRGBA16Sint           TextureFormat = 39
	TextureFormatRGBA16Float          TextureFormat = 40
	TextureFormatRGBA32Float          TextureFormat = 41
	TextureFormatRGBA32Uint           TextureFormat = 42
	TextureFormatRGBA32Sint           TextureFormat = 43
	TextureFormatStencil8             TextureFormat = 44
	TextureFormatDepth16Unorm         TextureFormat = 45
	TextureFormatDepth24Plus          TextureFormat = 46
	TextureFormatDepth24PlusStencil8  TextureFormat = 47
	TextureFormatDepth32Float         TextureFormat = 48
	TextureFormatDepth32FloatStencil8 TextureFormat = 49
	TextureFormatBC1RGBAUnorm         TextureFormat = 50
	TextureFormatBC1RGBAUnormSRGB     TextureFormat = 51
	TextureFormatBC2RGBAUnorm         TextureFormat = 52
	TextureFormatBC2RGBAUnormSRGB     TextureFormat = 53
	TextureFormatBC3RGBAUnorm         TextureFormat = 54
	TextureFormatBC3RGBAUnormSRGB     TextureFormat = 55
	TextureFormatBC4RUnorm            TextureFormat = 56
	TextureFormatBC4RSnorm            TextureFormat = 57
	TextureFormatBC5RGUnorm           TextureFormat = 58
	TextureFormatBC5RGSnorm           TextureFormat = 59
	TextureFormatBC6HRGBUfloat        TextureFormat = 60
	TextureFormatBC6HRGBFloat         TextureFormat = 61
	TextureFormatBC7RGBAUnorm         TextureFormat = 62
	TextureFormatBC7RGBAUnormSRGB     TextureFormat = 63
	TextureFormatETC2RGB8Unorm        TextureFormat = 64
	TextureFormatETC2RGB8UnormSRGB    TextureFormat = 65
	TextureFormatETC2RGB8A1Unorm      TextureFormat = 66
	TextureFormatETC2RGB8A1UnormSRGB  TextureFormat = 67
	TextureFormatETC2RGBA8Unorm       TextureFormat = 68
	TextureFormatETC2RGBA8UnormSRGB   TextureFormat = 69
	TextureFormatEACR11Unorm          TextureFormat = 70
	TextureFormatEACR11Snorm          TextureFormat = 71
	TextureFormatEACRG11Unorm         TextureFormat = 72
	TextureFormatEACRG11Snorm         TextureFormat = 73
	TextureFormatASTC4x4Unorm         TextureFormat = 74
	TextureFormatASTC4x4UnormSRGB     TextureFormat = 75
	TextureFormatASTC5x4Unorm         TextureFormat = 76
	TextureFormatASTC5x4UnormSRGB     TextureFormat = 77
	TextureFormatASTC5x5Unorm         TextureFormat = 78
	TextureFormatASTC5x5UnormSRGB     TextureFormat = 79
	TextureFormatASTC6x5Unorm         TextureFormat = 80
	TextureFormatASTC6x5UnormSRGB     TextureFormat = 81
	TextureFormatASTC6x6Unorm         TextureFormat = 82
	TextureFormatASTC6x6UnormSRGB     TextureFormat = 83
	TextureFormatASTC8x5Unorm         TextureFormat = 84
	TextureFormatASTC8x5UnormSRGB     TextureFormat = 85
	TextureFormatASTC8x6Unorm         TextureFormat = 86
	TextureFormatASTC8x6UnormSRGB     TextureFormat = 87
	TextureFormatASTC8x8Unorm         TextureFormat = 88
	TextureFormatASTC8x8UnormSRGB     TextureFormat = 89
	TextureFormatASTC10x5Unorm        TextureFormat = 90
	TextureFormatASTC10x5UnormSRGB    TextureFormat = 91
	TextureFormatASTC10x6Unorm        TextureFormat = 92
	TextureFormatASTC10x6UnormSRGB    TextureFormat = 93
	TextureFormatASTC10x8Unorm        TextureFormat = 94
	TextureFormatASTC10x8UnormSRGB    TextureFormat = 95
	TextureFormatASTC10x10Unorm       TextureFormat = 96
	TextureFormatASTC10x10UnormSRGB   TextureFormat = 97
	TextureFormatASTC12x10Unorm       TextureFormat = 98
	TextureFormatASTC12x10UnormSRGB   TextureFormat = 99
	TextureFormatASTC12x12Unorm       TextureFormat = 100
	TextureFormatASTC12x12UnormSRGB   TextureFormat = 101
)

// Size returns the number of bytes per texel.
// Returns 0 for block-compressed formats (BC, ETC2, EAC, ASTC), which pack
// a block of texels rather than a single one.
func (f TextureFormat) Size() int {
	switch f {
	// 1 byte
	case TextureFormatR8Unorm, TextureFormatR8Snorm, TextureFormatR8Uint, TextureFormatR8Sint,
		TextureFormatStencil8:
		return 1

	// 2 bytes
	case TextureFormatR16Unorm, TextureFormatR16Snorm, TextureFormatR16Uint, TextureFormatR16Sint, TextureFormatR16Float,
		TextureFormatRG8Unorm, TextureFormatRG8Snorm, TextureFormatRG8Uint, TextureFormatRG8Sint,
		TextureFormatDepth16Unorm:
		return 2

	// 4 bytes
	case TextureFormatR32Float, TextureFormatR32Uint, TextureFormatR32Sint,
		TextureFormatRG16Unorm, TextureFormatRG16Snorm, TextureFormatRG16Uint, TextureFormatRG16Sint, TextureFormatRG16Float,
		TextureFormatRGBA8Unorm, TextureFormatRGBA8UnormSRGB, TextureFormatRGBA8Snorm, TextureFormatRGBA8Uint, TextureFormatRGBA8Sint,
		TextureFormatBGRA8Unorm, TextureFormatBGRA8UnormSRGB,
		TextureFormatRGB10A2Uint, TextureFormatRGB10A2Unorm,
		TextureFormatRG11B10Ufloat, TextureFormatRGB9E5Ufloat,
		TextureFormatDepth24Plus, TextureFormatDepth24PlusStencil8, TextureFormatDepth32Float:
		return 4

	// 8 bytes
	case TextureFormatRG32Float, TextureFormatRG32Uint, TextureFormatRG32Sint,
		TextureFormatRGBA16Unorm, TextureFormatRGBA16Snorm, TextureFormatRGBA16Uint, TextureFormatRGBA16Sint, TextureFormatRGBA16Float,
		TextureFormatDepth32FloatStencil8:
		return 8

	// 16 bytes
	case TextureFormatRGBA32Float, TextureFormatRGBA32Uint, TextureFormatRGBA32Sint:
		return 16

	// Block-compressed: size is per block, not per texel
	default:
		return 0
	}
}

func (f TextureFormat) ToWGPU() wgpu.TextureFormat {
	return wgpu.TextureFormat(f)
}

var textureID idGen

type TextureData struct {
	id      uint32
	version int
	width   int
	height  int
	layers  int
	format  TextureFormat
	sampler Sampler

	pixels []byte

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
