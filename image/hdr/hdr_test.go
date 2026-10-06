package hdr_test

import (
	"bytes"
	"errors"
	"fmt"
	"image"
	"io"
	"math"
	"testing"

	"github.com/bluescreen10/pix/image/hdr"
)

// encodeRGBE packs linear light the way Radiance writes it: three mantissas scaled
// by the largest channel's power of two.
func encodeRGBE(c hdr.Color) [4]byte {
	largest := math.Max(float64(c.R), math.Max(float64(c.G), float64(c.B)))
	if largest < 1e-32 {
		return [4]byte{}
	}
	fraction, exponent := math.Frexp(largest)
	scale := fraction * 256 / largest
	return [4]byte{byte(float64(c.R) * scale), byte(float64(c.G) * scale), byte(float64(c.B) * scale), byte(exponent + 128)}
}

// rowEncoding is how a test file stores its rows.
type rowEncoding int

const (
	flatRows rowEncoding = iota
	runLengthRows
)

// encodeFile writes a Radiance file of pixels (rows top to bottom), with the given
// resolution line orientation ("-Y" or "+Y") and row encoding.
func encodeFile(pixels [][]hdr.Color, orientation string, encoding rowEncoding) []byte {
	var b bytes.Buffer
	height, width := len(pixels), len(pixels[0])
	fmt.Fprintf(&b, "#?RADIANCE\nFORMAT=32-bit_rle_rgbe\nEXPOSURE=1.0\n\n%s %d +X %d\n", orientation, height, width)
	for row := range height {
		y := row
		if orientation == "+Y" {
			y = height - 1 - row
		}
		rgbe := make([][4]byte, width)
		for x, c := range pixels[y] {
			rgbe[x] = encodeRGBE(c)
		}
		if encoding == flatRows {
			for _, p := range rgbe {
				b.Write(p[:])
			}
			continue
		}
		b.Write([]byte{2, 2, byte(width >> 8), byte(width)})
		for channel := range 4 {
			// The first half of the row as one repeated-byte run of its first value,
			// when the test made it uniform, then the rest as literals.
			half := width / 2
			b.Write([]byte{byte(128 + half), rgbe[0][channel]})
			b.WriteByte(byte(width - half))
			for _, p := range rgbe[half:] {
				b.WriteByte(p[channel])
			}
		}
	}
	return b.Bytes()
}

// testPixels is an 8 x 3 image (wide enough for run-length rows) whose left half of
// every row is uniform, and whose right half spans black to far beyond white.
func testPixels() [][]hdr.Color {
	values := []hdr.Color{{R: 0, G: 0, B: 0}, {R: 0.25, G: 0.5, B: 1}, {R: 3, G: 2, B: 1}, {R: 1000, G: 900, B: 10}}
	pixels := make([][]hdr.Color, 3)
	for y := range pixels {
		pixels[y] = make([]hdr.Color, 8)
		for x := range 4 {
			pixels[y][x] = hdr.Color{R: float32(y + 1), G: 0.5, B: 0.125}
		}
		for x := range 4 {
			pixels[y][4+x] = values[(x+y)%len(values)]
		}
	}
	return pixels
}

// isClose reports whether got is within RGBE's precision of want: a 256th of the
// pixel's largest channel, which sets the shared exponent.
func isClose(got, want hdr.Color) bool {
	largest := math.Max(float64(want.R), math.Max(float64(want.G), float64(want.B)))
	tolerance := largest/256 + 1e-6
	return math.Abs(float64(got.R-want.R)) <= tolerance &&
		math.Abs(float64(got.G-want.G)) <= tolerance &&
		math.Abs(float64(got.B-want.B)) <= tolerance
}

func TestDecode(t *testing.T) {
	for _, tc := range []struct {
		name        string
		orientation string
		encoding    rowEncoding
	}{
		{"flat", "-Y", flatRows},
		{"run-length", "-Y", runLengthRows},
		{"bottom-up", "+Y", runLengthRows},
	} {
		t.Run(tc.name, func(t *testing.T) {
			want := testPixels()
			img, format, err := image.Decode(bytes.NewReader(encodeFile(want, tc.orientation, tc.encoding)))
			if err != nil {
				t.Fatalf("image.Decode: %v", err)
			}
			if format != "hdr" {
				t.Errorf("format = %q, want %q", format, "hdr")
			}
			rgb, ok := img.(*hdr.RGB)
			if !ok {
				t.Fatalf("image.Decode returned %T, want *hdr.RGB", img)
			}
			if got := rgb.Bounds(); got != image.Rect(0, 0, 8, 3) {
				t.Fatalf("Bounds() = %v, want %v", got, image.Rect(0, 0, 8, 3))
			}
			for y, row := range want {
				for x, c := range row {
					if got := rgb.RGBAt(x, y); !isClose(got, c) {
						t.Errorf("RGBAt(%d, %d) = %v, want %v", x, y, got, c)
					}
				}
			}
		})
	}
}

// TestDecodeOldRunLength reads a flat row that repeats its pixels the old way: 1, 1, 1,
// n after a pixel repeats it n times, and a second such run straight after multiplies
// its count by 256.
func TestDecodeOldRunLength(t *testing.T) {
	const width = 300
	var b bytes.Buffer
	fmt.Fprintf(&b, "#?RADIANCE\n\n-Y 1 +X %d\n", width)
	red := encodeRGBE(hdr.Color{R: 2})
	blue := encodeRGBE(hdr.Color{B: 0.5})
	b.Write(red[:])
	b.Write([]byte{1, 1, 1, 33}) // 33 more red
	b.Write([]byte{1, 1, 1, 1})  // and 256 more: 290 in all
	b.Write(blue[:])
	b.Write([]byte{1, 1, 1, 9}) // 10 blue

	img, err := hdr.Decode(&b)
	if err != nil {
		t.Fatalf("Decode: %v", err)
	}
	rgb := img.(*hdr.RGB)
	for x := range width {
		want := hdr.Color{R: 2}
		if x >= 290 {
			want = hdr.Color{B: 0.5}
		}
		if got := rgb.RGBAt(x, 0); !isClose(got, want) {
			t.Fatalf("RGBAt(%d, 0) = %v, want %v", x, got, want)
		}
	}
}

func TestDecodeConfig(t *testing.T) {
	config, format, err := image.DecodeConfig(bytes.NewReader(encodeFile(testPixels(), "-Y", flatRows)))
	if err != nil {
		t.Fatalf("image.DecodeConfig: %v", err)
	}
	if format != "hdr" || config.Width != 8 || config.Height != 3 {
		t.Errorf("image.DecodeConfig = %q %dx%d, want %q 8x3", format, config.Width, config.Height, "hdr")
	}
	if config.ColorModel != hdr.ColorModel {
		t.Errorf("ColorModel is not hdr.ColorModel")
	}
}

func TestDecodeRejectsBadFiles(t *testing.T) {
	for _, tc := range []struct {
		name string
		file []byte
	}{
		{"not radiance", []byte("P6\n8 3\n255\n")},
		{"XYZE", []byte("#?RADIANCE\nFORMAT=32-bit_rle_xyze\n\n-Y 1 +X 1\n\x80\x80\x80\x80")},
		{"rotated", []byte("#?RADIANCE\n\n+X 1 -Y 1\n\x80\x80\x80\x80")},
		{"zero size", []byte("#?RADIANCE\n\n-Y 0 +X 1\n")},
		{"huge", []byte("#?RADIANCE\n\n-Y 100000000 +X 100000000\n")},
		{"no resolution", []byte("#?RADIANCE\n\n")},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if _, err := hdr.Decode(bytes.NewReader(tc.file)); err == nil {
				t.Errorf("Decode succeeded, want an error")
			}
		})
	}
}

func TestDecodeTruncatedIsUnexpectedEOF(t *testing.T) {
	valid := encodeFile(testPixels(), "-Y", runLengthRows)
	_, err := hdr.Decode(bytes.NewReader(valid[:len(valid)-5]))
	if !errors.Is(err, io.ErrUnexpectedEOF) {
		t.Errorf("Decode of a truncated file: %v, want io.ErrUnexpectedEOF", err)
	}
}

// TestColorRGBA checks that the standard library's view of an HDR pixel is clipped to
// white and sRGB-encoded, as 8- and 16-bit images hold colour.
func TestColorRGBA(t *testing.T) {
	r, g, b, a := hdr.Color{R: 5, G: 0.214, B: 0}.RGBA()
	// Linear 0.214 is sRGB 0.5.
	if r != 0xffff || g < 0x7f00 || g > 0x8100 || b != 0 || a != 0xffff {
		t.Errorf("Color{5, 0.214, 0}.RGBA() = %#x %#x %#x %#x, want 0xffff ~0x8000 0 0xffff", r, g, b, a)
	}
}
