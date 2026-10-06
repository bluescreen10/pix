package textures

import (
	"encoding/binary"
	"image"
	"math"

	"github.com/bluescreen10/pix/image/hdr"
)

// linearRGB is img's light as tightly packed float32 RGB: an *hdr.RGB's own pixels, or
// any other image's colour decoded from sRGB.
func linearRGB(img image.Image) []float32 {
	bounds := img.Bounds()
	w, h := bounds.Dx(), bounds.Dy()
	if rgb, ok := img.(*hdr.RGB); ok && rgb.Stride == w*3 && bounds.Min == (image.Point{}) {
		return rgb.Pix[:w*h*3]
	}
	out := make([]float32, 0, w*h*3)
	for y := bounds.Min.Y; y < bounds.Max.Y; y++ {
		for x := bounds.Min.X; x < bounds.Max.X; x++ {
			c := hdr.ColorModel.Convert(img.At(x, y)).(hdr.Color)
			out = append(out, c.R, c.G, c.B)
		}
	}
	return out
}

// prepareHDR packs linear RGB light (w*h*3 float32s, row-major) as an HDR texture and
// builds the levels mips asks for. Each level is averaged from the one above in float,
// before packing, so a small, very bright sun keeps its light at every level instead of
// being rounded away.
func prepareHDR(rgb []float32, w, h int, mips MipChain) Image {
	img := Image{format: HDR, width: w, height: h, levels: [][]byte{packRGB9E5(rgb)}}
	if mips == BaseLevelOnly {
		return img
	}
	src, sw, sh := rgb, w, h
	for sw > 1 || sh > 1 {
		dw, dh := max(sw/2, 1), max(sh/2, 1)
		src = downsampleRGB(src, sw, sh, dw, dh)
		img.levels = append(img.levels, packRGB9E5(src))
		sw, sh = dw, dh
	}
	return img
}

// downsampleRGB box-filters float RGB src (sw×sh) into a new dw×dh image, with the same
// footprint as downsample: an axis that doesn't halve samples a single texel.
func downsampleRGB(src []float32, sw, sh, dw, dh int) []float32 {
	dst := make([]float32, dw*dh*3)
	xStep, yStep := 2, 2
	if dw == sw {
		xStep = 1
	}
	if dh == sh {
		yStep = 1
	}
	weight := 1 / float32(xStep*yStep)

	for y := range dh {
		for x := range dw {
			o := (y*dw + x) * 3
			for dy := range yStep {
				sy := min(y*yStep+dy, sh-1)
				for dx := range xStep {
					sx := min(x*xStep+dx, sw-1)
					i := (sy*sw + sx) * 3
					dst[o] += src[i] * weight
					dst[o+1] += src[i+1] * weight
					dst[o+2] += src[i+2] * weight
				}
			}
		}
	}
	return dst
}

// packRGB9E5 packs float RGB as RGB9E5 texels, four little-endian bytes each.
func packRGB9E5(rgb []float32) []byte {
	out := make([]byte, len(rgb)/3*4)
	for i := range len(rgb) / 3 {
		binary.LittleEndian.PutUint32(out[i*4:], encodeRGB9E5(rgb[i*3], rgb[i*3+1], rgb[i*3+2]))
	}
	return out
}

// RGB9E5 holds three 9-bit mantissas, red in the low bits, and a 5-bit exponent
// shared by all three in the top five.
const (
	rgb9e5MantissaBits = 9
	rgb9e5ExponentBias = 15
	rgb9e5MaxExponent  = 31
	// rgb9e5Max is the largest value RGB9E5 holds: 511/512 * 2^16.
	rgb9e5Max = float64(1<<rgb9e5MantissaBits-1) / (1 << rgb9e5MantissaBits) * (1 << (rgb9e5MaxExponent - rgb9e5ExponentBias))
)

// encodeRGB9E5 packs linear light, clamped to [0, rgb9e5Max], as one RGB9E5 texel, by
// the rounding EXT_texture_shared_exponent specifies: the exponent the largest channel
// needs, raised by one when rounding its mantissa would carry out of nine bits.
func encodeRGB9E5(r, g, b float32) uint32 {
	red, green, blue := clampRGB9E5(r), clampRGB9E5(g), clampRGB9E5(b)
	largest := max(red, green, blue)
	if largest == 0 {
		return 0
	}

	exponent := max(-rgb9e5ExponentBias-1, int(math.Floor(math.Log2(largest)))) + 1 + rgb9e5ExponentBias
	step := math.Ldexp(1, exponent-rgb9e5ExponentBias-rgb9e5MantissaBits)
	if math.Floor(largest/step+0.5) == 1<<rgb9e5MantissaBits {
		exponent++
		step *= 2
	}

	mantissa := func(v float64) uint32 {
		return uint32(math.Floor(v/step + 0.5))
	}
	return mantissa(red) | mantissa(green)<<9 | mantissa(blue)<<18 | uint32(exponent)<<27
}

// clampRGB9E5 clamps v to what RGB9E5 holds; NaN becomes 0.
func clampRGB9E5(v float32) float64 {
	if !(v > 0) {
		return 0
	}
	return math.Min(float64(v), rgb9e5Max)
}
