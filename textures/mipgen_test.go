package textures_test

import (
	"encoding/binary"
	"image"
	"math"
	"testing"

	"github.com/bluescreen10/pix/image/hdr"
	"github.com/bluescreen10/pix/textures"
)

// TestMipLevelCount pins the chain length, including the non-square case where one
// axis bottoms out at 1 before the other.
func TestMipLevelCount(t *testing.T) {
	cases := []struct{ w, h, want int }{
		{1, 1, 1}, {2, 2, 2}, {4, 4, 3}, {256, 256, 9},
		{8, 2, 4},  // 8x2 -> 4x1 -> 2x1 -> 1x1
		{16, 1, 5}, // 16x1 -> 8x1 -> 4x1 -> 2x1 -> 1x1
	}
	for _, c := range cases {
		rgba := make([]byte, c.w*c.h*4)
		_, levels, _ := textures.GenerateMipChain(nrgbaImage(rgba, c.w, c.h), textures.Linear)
		if got := len(levels) + 1; got != c.want {
			t.Errorf("GenerateMipChain(%d,%d) produced %d levels, want %d", c.w, c.h, got, c.want)
		}
	}
}

// TestMipChainShape checks each level halves (floored at 1) and is fully sized.
func TestMipChainShape(t *testing.T) {
	const w, h = 8, 4
	rgba := make([]byte, w*h*4)
	_, levels, sizes := textures.GenerateMipChain(nrgbaImage(rgba, w, h), textures.Linear)
	want := [][2]int{{4, 2}, {2, 1}, {1, 1}}
	if len(levels) != len(want) {
		t.Fatalf("got %d levels, want %d", len(levels), len(want))
	}
	for i := range want {
		if sizes[i] != want[i] {
			t.Fatalf("level %d size = %v, want %v", i, sizes[i], want[i])
		}
		if n := want[i][0] * want[i][1] * 4; len(levels[i]) != n {
			t.Fatalf("level %d has %d bytes, want %d", i, len(levels[i]), n)
		}
	}
}

// TestSRGBMipsFilterInLinearSpace is the trap this code exists to avoid. A
// checkerboard of black and white averages to 0.5 LINEAR, which is ~188 when
// re-encoded to sRGB (IEC 61966-2-1) — not 128. Filtering the encoded bytes directly
// would give ~128, i.e. a visibly darker mip. Anything near 128 means the
// linearization was skipped.
func TestSRGBMipsFilterInLinearSpace(t *testing.T) {
	const w, h = 2, 2
	rgba := []byte{
		0, 0, 0, 255, 255, 255, 255, 255,
		255, 255, 255, 255, 0, 0, 0, 255,
	}
	_, levels, _ := textures.GenerateMipChain(nrgbaImage(rgba, w, h), textures.SRGB)
	got := levels[0][0]

	const want = 188 // linearToSRGB(0.5) * 255, IEC 61966-2-1
	if got < want-2 || got > want+2 {
		t.Fatalf("sRGB mip of black/white checkerboard = %d, want ~%d "+
			"(got ~128? then filtering happened in sRGB space, not linear)", got, want)
	}
	if got >= 120 && got <= 136 {
		t.Fatalf("mip value %d is the naive sRGB-space average — mips will be too dark", got)
	}
}

// TestLinearMipsDoNotGammaCorrect is the converse: data maps (roughness, packed
// ORM) must be averaged raw. Applying the sRGB curve to them would corrupt values
// that were never gamma-encoded.
func TestLinearMipsDoNotGammaCorrect(t *testing.T) {
	const w, h = 2, 2
	rgba := []byte{
		0, 0, 0, 255, 255, 255, 255, 255,
		255, 255, 255, 255, 0, 0, 0, 255,
	}
	_, levels, _ := textures.GenerateMipChain(nrgbaImage(rgba, w, h), textures.Linear)
	if got := levels[0][0]; got < 127 || got > 128 {
		t.Fatalf("linear mip of 0/255 checkerboard = %d, want 127-128 (a plain average)", got)
	}
}

// TestNormalMipsStayUnitLength is the other silent trap: averaging two unit vectors
// yields a shorter one, so without renormalization every mip level's normals shrink
// and specular dulls with distance.
func TestNormalMipsStayUnitLength(t *testing.T) {
	// Two channels (RG8, Z implied) packed into RGBA8 source pixels (repack keeps
	// only the leading two), with neighbouring texels tilted hard in opposite
	// directions so a plain average would shorten badly.
	const w, h = 2, 2
	enc := func(v float32) byte {
		return byte((v*0.5+0.5)*255 + 0.5)
	}
	ax, ay := enc(0.8), enc(0)
	bx, by := enc(-0.8), enc(0)
	rgba := []byte{
		ax, ay, 0, 0, bx, by, 0, 0,
		bx, by, 0, 0, ax, ay, 0, 0,
	}

	_, levels, _ := textures.GenerateMipChain(nrgbaImage(rgba, w, h), textures.Normal)
	x := float64(levels[0][0])/255*2 - 1
	y := float64(levels[0][1])/255*2 - 1
	z := math.Sqrt(math.Max(1-x*x-y*y, 0))
	if l := math.Sqrt(x*x + y*y + z*z); l < 0.99 || l > 1.01 {
		t.Fatalf("normal mip length = %v, want ~1 (normals were not renormalized)", l)
	}
}

// TestRepackNarrowsChannels checks the RGBA-in, narrow-format-out conversion that lets
// callers hand every texture over as RGBA regardless of its GPU format.
func TestRepackNarrowsChannels(t *testing.T) {
	rgba := []byte{10, 20, 30, 40, 50, 60, 70, 80}

	base, _, _ := textures.GenerateMipChain(nrgbaImage(rgba, 2, 1), textures.Normal)
	if string(base) != string([]byte{10, 20, 50, 60}) {
		t.Fatalf("Normal base = %v, want [10 20 50 60]", base)
	}

	base, _, _ = textures.GenerateMipChain(nrgbaImage(rgba, 2, 1), textures.Grayscale)
	if string(base) != string([]byte{10, 50}) {
		t.Fatalf("Grayscale base = %v, want [10 50]", base)
	}

	base, _, _ = textures.GenerateMipChain(nrgbaImage(rgba, 2, 1), textures.SRGB)
	if string(base) != string(rgba) {
		t.Fatalf("SRGB base = %v, want %v (RGBA passes through unchanged)", base, rgba)
	}
}

// TestSRGBMipsKeepUniformColours: mipmapping a uniform sRGB image leaves every level the
// same colour, for each of the 256 values a channel can hold — the decoding and encoding
// tables round-trip exactly.
func TestSRGBMipsKeepUniformColours(t *testing.T) {
	for v := range 256 {
		rgba := make([]byte, 4*4*4)
		for i := 0; i < len(rgba); i += 4 {
			rgba[i], rgba[i+1], rgba[i+2], rgba[i+3] = byte(v), byte(v), byte(v), 255
		}
		_, levels, _ := textures.GenerateMipChain(nrgbaImage(rgba, 4, 4), textures.SRGB)
		for l, level := range levels {
			if level[0] != byte(v) {
				t.Fatalf("level %d of a uniform image of %d = %d, want %d", l+1, v, level[0], v)
			}
		}
	}
}

// TestPrepareBuildsTheLevelsAsked: Prepare makes the whole chain by default, and the
// base level alone with BaseLevelOnly.
func TestPrepareBuildsTheLevelsAsked(t *testing.T) {
	rgba := make([]byte, 8*8*4)
	full := textures.Prepare(nrgbaImage(rgba, 8, 8), textures.Linear, textures.FullMipChain)
	// 8x8, 4x4, 2x2, 1x1.
	if got, want := full.Levels(), 4; got != want {
		t.Errorf("Prepare(FullMipChain).Levels() = %d, want %d", got, want)
	}
	base := textures.Prepare(nrgbaImage(rgba, 8, 8), textures.Linear, textures.BaseLevelOnly)
	if got := base.Levels(); got != 1 {
		t.Errorf("Prepare(BaseLevelOnly).Levels() = %d, want 1", got)
	}
}

// nrgbaImage wraps raw RGBA bytes, w*h*4 of them, as an image, without copying them.
func nrgbaImage(pixels []byte, w, h int) *image.NRGBA {
	return &image.NRGBA{Pix: pixels, Stride: w * 4, Rect: image.Rect(0, 0, w, h)}
}

// TestGrayscaleTakesGrayBytes: one-channel data handed over as an image.Gray becomes
// a Grayscale texture byte for byte, rows read by its stride — including one cut from a
// larger image, whose rows are not contiguous.
func TestGrayscaleTakesGrayBytes(t *testing.T) {
	gray := &image.Gray{Pix: []byte{10, 20, 30, 40, 50, 60}, Stride: 3, Rect: image.Rect(0, 0, 3, 2)}
	base, _, _ := textures.GenerateMipChain(gray, textures.Grayscale)
	if string(base) != string(gray.Pix) {
		t.Errorf("Grayscale base of a Gray image = %v, want %v", base, gray.Pix)
	}

	// The right two columns of it.
	cropped := gray.SubImage(image.Rect(1, 0, 3, 2))
	base, _, _ = textures.GenerateMipChain(cropped, textures.Grayscale)
	if want := []byte{20, 30, 50, 60}; string(base) != string(want) {
		t.Errorf("Grayscale base of a cropped Gray image = %v, want %v", base, want)
	}
}

// TestGrayImageAsColour: a one-channel image made into a colour texture is grey, its
// value in red, green and blue, and opaque.
func TestGrayImageAsColour(t *testing.T) {
	gray := &image.Gray{Pix: []byte{10, 200}, Stride: 2, Rect: image.Rect(0, 0, 2, 1)}
	base, _, _ := textures.GenerateMipChain(gray, textures.Linear)
	if want := []byte{10, 10, 10, 255, 200, 200, 200, 255}; string(base) != string(want) {
		t.Errorf("Linear base of a Gray image = %v, want %v", base, want)
	}
}

// decodeRGB9E5 unpacks one RGB9E5 texel: three 9-bit mantissas, red lowest, scaled by
// 2^(exponent - 15 - 9).
func decodeRGB9E5(texel []byte) [3]float64 {
	v := binary.LittleEndian.Uint32(texel)
	scale := math.Ldexp(1, int(v>>27)-15-9)
	return [3]float64{float64(v&511) * scale, float64(v>>9&511) * scale, float64(v>>18&511) * scale}
}

// TestHDRKeepsLightBeyondWhite: an HDR texture holds an *hdr.RGB's light as it is, far
// past 1, to RGB9E5's precision — nine bits of the largest channel.
func TestHDRKeepsLightBeyondWhite(t *testing.T) {
	light := hdr.NewRGB(image.Rect(0, 0, 2, 1))
	copy(light.Pix, []float32{4, 0.5, 0, 30000, 1, 0.001})
	base, _, _ := textures.GenerateMipChain(light, textures.HDR)
	if len(base) != 2*4 {
		t.Fatalf("HDR base is %d bytes, want 8: four a texel", len(base))
	}
	for i, want := range [][3]float64{{4, 0.5, 0}, {30000, 1, 0.001}} {
		got := decodeRGB9E5(base[i*4:])
		tolerance := math.Max(want[0], math.Max(want[1], want[2])) / 512
		for c := range 3 {
			if math.Abs(got[c]-want[c]) > tolerance {
				t.Errorf("texel %d = %v, want %v", i, got, want)
				break
			}
		}
	}
}

// TestHDRMipsKeepASmallSunsLight: one texel of light 1000 among three of 0 averages to
// 250 in the next level. Averaged after packing, or clipped first, it would not.
func TestHDRMipsKeepASmallSunsLight(t *testing.T) {
	light := hdr.NewRGB(image.Rect(0, 0, 2, 2))
	copy(light.Pix, []float32{1000, 1000, 1000})
	_, levels, _ := textures.GenerateMipChain(light, textures.HDR)
	if got := decodeRGB9E5(levels[0]); math.Abs(got[0]-250) > 1 {
		t.Errorf("mip of one 1000 texel among three black = %v, want 250", got)
	}
}
