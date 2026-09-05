package textures

import "github.com/bluescreen10/pix/gpu"

// Format is what a texture is *for*, which is what decides both its GPU format and
// how its mips must be filtered. It is not a raw format enum: callers know they have
// a normal map, not that they want two unorm channels.
//
// Source pixels are always handed in as RGBA8; Store.Create repacks to the narrower
// formats, so callers never have to pre-swizzle.
type Format uint8

const (
	// SRGB is 8-bit sRGB color (base color, emissive) — the GPU decodes to
	// linear on sample, and mips are filtered in linear space.
	SRGB Format = iota
	// Linear is 8-bit linear RGBA data sampled as-is (packed
	// metallic-roughness-occlusion, lookup tables).
	Linear
	// Normal is a tangent-space normal map, stored as two channels: Z is a
	// unit vector's implied third component, so storing it wastes a quarter of the
	// texture. Shaders reconstruct it (see scene_pbr.frag.glsl), and mips are
	// renormalized rather than plainly averaged.
	Normal
	// Grayscale is a single-channel 8-bit map (occlusion, a standalone
	// roughness mask) — the red channel of the source is kept.
	Grayscale
)

func (f Format) gpuFormat() gpu.Format {
	switch f {
	case Linear:
		return gpu.FormatRGBA8Unorm
	case Normal:
		return gpu.FormatRG8Unorm
	case Grayscale:
		return gpu.FormatR8Unorm
	default:
		return gpu.FormatRGBA8Srgb
	}
}

// channels is how many bytes per texel this format stores on the GPU.
func (f Format) channels() int {
	switch f {
	case Normal:
		return 2
	case Grayscale:
		return 1
	default:
		return 4
	}
}

// repack narrows RGBA8 source pixels to this format's channel count, keeping the
// leading channels (RG for a normal map, R for grayscale). RGBA formats pass the
// source through untouched.
func (f Format) repack(rgba []byte, w, h int) []byte {
	c := f.channels()
	if c == 4 {
		return rgba
	}
	out := make([]byte, w*h*c)
	for i := range w * h {
		copy(out[i*c:(i+1)*c], rgba[i*4:i*4+c])
	}
	return out
}
