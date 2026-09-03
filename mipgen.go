package pix

import "github.com/chewxy/math32"

// Mip chain generation. Downsampling looks trivial — average four texels — and is
// wrong in two ways that both fail silently, so both are handled here rather than
// left to callers:
//
//   - sRGB images must be filtered in LINEAR space. Averaging sRGB-encoded bytes
//     darkens every level: the mean of 0 and 255 is 128, which decodes to ~0.216
//     linear, not 0.5. Decode, average, re-encode.
//   - Normal maps must be RENORMALIZED after averaging. The mean of two unit
//     vectors is shorter than one, so without this every level's normals shrink
//     and specular quietly dulls with distance.

// mipLevels is how many levels a w×h image has, down to 1×1.
func mipLevels(w, h int) int {
	n := 1
	for w > 1 || h > 1 {
		w, h = max(w/2, 1), max(h/2, 1)
		n++
	}
	return n
}

// mipChain builds every level below the base for a `channels`-per-texel image,
// returning them largest-first (NOT including the base). srgb selects linear-space
// filtering; normalMap renormalizes each result texel as a unit vector.
//
// Each level is filtered from the level above, not from the base: the error of a
// repeated 2×2 box filter is what every offline tool produces too, and filtering
// from the base each time would cost far more for no visible gain at these sizes.
func mipChain(base []byte, w, h, channels int, srgb, normalMap bool) (levels [][]byte, sizes [][2]int) {
	src, sw, sh := base, w, h
	for sw > 1 || sh > 1 {
		dw, dh := max(sw/2, 1), max(sh/2, 1)
		dst := downsample(src, sw, sh, dw, dh, channels, srgb, normalMap)
		levels = append(levels, dst)
		sizes = append(sizes, [2]int{dw, dh})
		src, sw, sh = dst, dw, dh
	}
	return levels, sizes
}

// downsample box-filters src (sw×sh) into a new dw×dh image. When an axis doesn't
// halve (it was already 1) that axis samples a single texel rather than two.
func downsample(src []byte, sw, sh, dw, dh, channels int, srgb, normalMap bool) []byte {
	dst := make([]byte, dw*dh*channels)
	xStep, yStep := 2, 2
	if dw == sw {
		xStep = 1
	}
	if dh == sh {
		yStep = 1
	}

	var acc [4]float32
	for y := range dh {
		for x := range dw {
			var n float32
			acc = [4]float32{}
			for dy := range yStep {
				sy := min(y*yStep+dy, sh-1)
				for dx := range xStep {
					sx := min(x*xStep+dx, sw-1)
					i := (sy*sw + sx) * channels
					for c := range channels {
						v := float32(src[i+c]) / 255
						// Alpha is never sRGB-encoded, even in an sRGB image.
						if srgb && c < 3 {
							v = srgbToLinear(v)
						}
						acc[c] += v
					}
					n++
				}
			}

			o := (y*dw + x) * channels
			if normalMap {
				// Rebuild a unit vector from the averaged components. Stored
				// normals are [0,1]-encoded, so decode to [-1,1] first.
				var v [3]float32
				for c := range min(channels, 3) {
					v[c] = (acc[c]/n)*2 - 1
				}
				if channels < 3 {
					// RG-only normal map: Z is implied, and only X/Y are stored,
					// so normalize against the reconstructed Z.
					v[2] = math32.Sqrt(max(1-v[0]*v[0]-v[1]*v[1], 0))
				}
				if l := math32.Sqrt(v[0]*v[0] + v[1]*v[1] + v[2]*v[2]); l > 0 {
					v[0], v[1], v[2] = v[0]/l, v[1]/l, v[2]/l
				}
				for c := range min(channels, 3) {
					dst[o+c] = encodeUnorm8(v[c]*0.5 + 0.5)
				}
				for c := 3; c < channels; c++ {
					dst[o+c] = encodeUnorm8(acc[c] / n)
				}
				continue
			}

			for c := range channels {
				v := acc[c] / n
				if srgb && c < 3 {
					v = linearToSRGB(v)
				}
				dst[o+c] = encodeUnorm8(v)
			}
		}
	}
	return dst
}

func encodeUnorm8(v float32) byte {
	return byte(min(max(v, 0), 1)*255 + 0.5)
}

// srgbToLinear / linearToSRGB are the exact IEC 61966-2-1 transfer functions (the
// piecewise ones, not a 2.2 power approximation — the toe matters in dark texels,
// which is where mip darkening would show first).
func srgbToLinear(v float32) float32 {
	if v <= 0.04045 {
		return v / 12.92
	}
	return math32.Pow((v+0.055)/1.055, 2.4)
}

func linearToSRGB(v float32) float32 {
	if v <= 0.0031308 {
		return v * 12.92
	}
	return 1.055*math32.Pow(v, 1/2.4) - 0.055
}
