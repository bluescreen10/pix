package textures

import (
	"math"
	"testing"
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
		if got := mipLevels(c.w, c.h); got != c.want {
			t.Errorf("mipLevels(%d,%d) = %d, want %d", c.w, c.h, got, c.want)
		}
	}
}

// TestMipChainShape checks each level halves (floored at 1) and is fully sized.
func TestMipChainShape(t *testing.T) {
	const w, h, ch = 8, 4, 4
	base := make([]byte, w*h*ch)
	levels, sizes := mipChain(base, w, h, ch, false, false)
	want := [][2]int{{4, 2}, {2, 1}, {1, 1}}
	if len(levels) != len(want) {
		t.Fatalf("got %d levels, want %d", len(levels), len(want))
	}
	for i := range want {
		if sizes[i] != want[i] {
			t.Fatalf("level %d size = %v, want %v", i, sizes[i], want[i])
		}
		if n := want[i][0] * want[i][1] * ch; len(levels[i]) != n {
			t.Fatalf("level %d has %d bytes, want %d", i, len(levels[i]), n)
		}
	}
}

// TestSRGBMipsFilterInLinearSpace is the trap this code exists to avoid. A
// checkerboard of black and white averages to 0.5 LINEAR, which is ~188 when
// re-encoded to sRGB — not 128. Filtering the encoded bytes directly would give
// ~128, i.e. a visibly darker mip. Anything near 128 means the linearization was
// skipped.
func TestSRGBMipsFilterInLinearSpace(t *testing.T) {
	const w, h, ch = 2, 2, 4
	base := []byte{
		0, 0, 0, 255, 255, 255, 255, 255,
		255, 255, 255, 255, 0, 0, 0, 255,
	}
	levels, _ := mipChain(base, w, h, ch, true, false)
	got := levels[0][0]

	want := byte(linearToSRGB(0.5)*255 + 0.5)
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
	const w, h, ch = 2, 2, 4
	base := []byte{
		0, 0, 0, 255, 255, 255, 255, 255,
		255, 255, 255, 255, 0, 0, 0, 255,
	}
	levels, _ := mipChain(base, w, h, ch, false, false)
	if got := levels[0][0]; got < 127 || got > 128 {
		t.Fatalf("linear mip of 0/255 checkerboard = %d, want 127-128 (a plain average)", got)
	}
}

// TestNormalMipsStayUnitLength is the other silent trap: averaging two unit vectors
// yields a shorter one, so without renormalization every mip level's normals shrink
// and specular dulls with distance.
func TestNormalMipsStayUnitLength(t *testing.T) {
	// Two channels (RG8, Z implied), with neighbouring texels tilted hard in
	// opposite directions so a plain average would shorten badly.
	const w, h, ch = 2, 2, 2
	enc := func(x, y float32) (byte, byte) {
		return encodeUnorm8(x*0.5 + 0.5), encodeUnorm8(y*0.5 + 0.5)
	}
	ax, ay := enc(0.8, 0)
	bx, by := enc(-0.8, 0)
	base := []byte{ax, ay, bx, by, bx, by, ax, ay}

	levels, _ := mipChain(base, w, h, ch, false, true)
	x := float64(levels[0][0])/255*2 - 1
	y := float64(levels[0][1])/255*2 - 1
	z := math.Sqrt(math.Max(1-x*x-y*y, 0))
	if l := math.Sqrt(x*x + y*y + z*z); l < 0.99 || l > 1.01 {
		t.Fatalf("normal mip length = %v, want ~1 (normals were not renormalized)", l)
	}
}

// TestRepackNarrowsChannels checks the RGBA8-in, narrow-format-out conversion that
// lets callers hand every texture over as RGBA8 regardless of its GPU format.
func TestRepackNarrowsChannels(t *testing.T) {
	rgba := []byte{10, 20, 30, 40, 50, 60, 70, 80}
	if got := Normal.repack(rgba, 2, 1); string(got) != string([]byte{10, 20, 50, 60}) {
		t.Fatalf("Normal.repack = %v, want [10 20 50 60]", got)
	}
	if got := Grayscale.repack(rgba, 2, 1); string(got) != string([]byte{10, 50}) {
		t.Fatalf("Grayscale.repack = %v, want [10 50]", got)
	}
	if got := SRGB.repack(rgba, 2, 1); &got[0] != &rgba[0] {
		t.Fatal("SRGB.repack should pass RGBA through without copying")
	}
	if Normal.channels() != 2 || Grayscale.channels() != 1 || SRGB.channels() != 4 {
		t.Fatal("channel counts do not match the formats")
	}
}
