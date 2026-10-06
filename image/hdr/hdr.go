// Package hdr decodes Radiance RGBE images (.hdr): high-dynamic-range light, the format
// environment panoramas are usually distributed in. Importing it registers the format
// with the image package, so that image.Decode reads such files:
//
//	import _ "github.com/bluescreen10/pix/image/hdr"
//
// A decoded image is an *RGB: linear light as float32, unbounded above.
//
// Each pixel is stored as three 8-bit mantissas sharing an 8-bit exponent, which keeps
// about 1% precision over an enormous range. Rows are either flat or run-length encoded,
// in the old per-pixel scheme or the newer per-channel one; all three are read. Only
// the RGB primaries are supported (FORMAT=32-bit_rle_rgbe, not XYZE), in the standard
// top-down layout (-Y h +X w) or bottom-up (+Y h +X w). EXPOSURE and other header
// variables are ignored, as most readers ignore them.
package hdr

import (
	"bufio"
	"errors"
	"fmt"
	"image"
	"io"
	"math"
	"strconv"
	"strings"
)

func init() {
	image.RegisterFormat("hdr", "#?", Decode, DecodeConfig)
}

// maxSide bounds each side of an image the decoder accepts, so that a corrupt or
// hostile header cannot make it allocate without limit. Panoramas reach 16384 wide.
const maxSide = 1 << 16

// header is what an image's header says about its pixels.
type header struct {
	width, height int
	// isBottomUp is set for +Y files, whose first row is the bottom one.
	isBottomUp bool
}

// Decode reads a Radiance RGBE image from r.
func Decode(r io.Reader) (image.Image, error) {
	br := bufio.NewReader(r)
	h, err := readHeader(br)
	if err != nil {
		return nil, err
	}

	img := NewRGB(image.Rect(0, 0, h.width, h.height))
	scanline := make([]byte, h.width*4)
	for row := range h.height {
		if err := readScanline(br, scanline); err != nil {
			return nil, fmt.Errorf("hdr: row %d: %w", row, err)
		}
		y := row
		if h.isBottomUp {
			y = h.height - 1 - row
		}
		decodeScanline(scanline, img.Pix[y*img.Stride:(y+1)*img.Stride])
	}
	return img, nil
}

// DecodeConfig reads the size of a Radiance RGBE image from r, without its pixels.
func DecodeConfig(r io.Reader) (image.Config, error) {
	h, err := readHeader(bufio.NewReader(r))
	if err != nil {
		return image.Config{}, err
	}
	return image.Config{ColorModel: ColorModel, Width: h.width, Height: h.height}, nil
}

// readHeader reads the header up to and including the resolution line, leaving r at
// the first row of pixels.
func readHeader(r *bufio.Reader) (header, error) {
	line, err := readHeaderLine(r)
	if err != nil {
		return header{}, err
	}
	if !strings.HasPrefix(line, "#?") {
		return header{}, errors.New("hdr: not a Radiance image")
	}
	// Variables, one a line, up to a blank line.
	for {
		line, err := readHeaderLine(r)
		if err != nil {
			return header{}, err
		}
		if line == "" {
			break
		}
		if format, ok := strings.CutPrefix(line, "FORMAT="); ok && format != "32-bit_rle_rgbe" {
			return header{}, fmt.Errorf("hdr: unsupported format %q", format)
		}
	}

	line, err = readHeaderLine(r)
	if err != nil {
		return header{}, err
	}
	return parseResolution(line)
}

// readHeaderLine reads one line of the header, without its line ending.
func readHeaderLine(r *bufio.Reader) (string, error) {
	line, err := r.ReadString('\n')
	if err != nil {
		if err == io.EOF {
			err = io.ErrUnexpectedEOF
		}
		return "", fmt.Errorf("hdr: header: %w", err)
	}
	return strings.TrimRight(line, "\r\n"), nil
}

// parseResolution parses the resolution line: "-Y h +X w" for an image stored top
// down, "+Y h +X w" for one stored bottom up.
func parseResolution(line string) (header, error) {
	fields := strings.Fields(line)
	if len(fields) != 4 || (fields[0] != "-Y" && fields[0] != "+Y") || fields[2] != "+X" {
		return header{}, fmt.Errorf("hdr: unsupported resolution line %q", line)
	}
	height, errHeight := strconv.Atoi(fields[1])
	width, errWidth := strconv.Atoi(fields[3])
	if errHeight != nil || errWidth != nil || width <= 0 || height <= 0 || width > maxSide || height > maxSide {
		return header{}, fmt.Errorf("hdr: invalid resolution %q", line)
	}
	return header{width: width, height: height, isBottomUp: fields[0] == "+Y"}, nil
}

// readScanline reads one row of RGBE pixels into scanline, four bytes a pixel.
//
// A row in the newer run-length encoding starts with 2, 2 and the row's width in 15
// bits; then each channel is encoded on its own, as runs of one repeated byte or of
// literal bytes. Rows narrower than 8 or wider than 32767 cannot be encoded that way,
// and any other row may be flat, so a row that does not start so is read flat.
func readScanline(r *bufio.Reader, scanline []byte) error {
	width := len(scanline) / 4
	if width < 8 || width > 0x7fff {
		return readFlatScanline(r, scanline)
	}
	start, err := r.Peek(4)
	if err != nil {
		return unexpectedEOF(err)
	}
	if start[0] != 2 || start[1] != 2 || start[2]&0x80 != 0 {
		return readFlatScanline(r, scanline)
	}
	if encodedWidth := int(start[2])<<8 | int(start[3]); encodedWidth != width {
		return fmt.Errorf("encoded width %d, want %d", encodedWidth, width)
	}
	if _, err := r.Discard(4); err != nil {
		return unexpectedEOF(err)
	}

	var literals [128]byte
	for channel := range 4 {
		for x := 0; x < width; {
			count, err := r.ReadByte()
			if err != nil {
				return unexpectedEOF(err)
			}
			if count > 128 {
				run := int(count) - 128
				if x+run > width {
					return errors.New("run past the end of the row")
				}
				value, err := r.ReadByte()
				if err != nil {
					return unexpectedEOF(err)
				}
				for range run {
					scanline[x*4+channel] = value
					x++
				}
				continue
			}
			n := int(count)
			if n == 0 || x+n > width {
				return errors.New("literal run past the end of the row")
			}
			if _, err := io.ReadFull(r, literals[:n]); err != nil {
				return unexpectedEOF(err)
			}
			for _, value := range literals[:n] {
				scanline[x*4+channel] = value
				x++
			}
		}
	}
	return nil
}

// readFlatScanline reads a row stored pixel by pixel, which may use the old run-length
// encoding: a pixel of 1, 1, 1, n repeats the one before it n times, and each such
// pixel straight after another multiplies its count by 256.
func readFlatScanline(r *bufio.Reader, scanline []byte) error {
	width := len(scanline) / 4
	shift := 0
	for x := 0; x < width; {
		var pixel [4]byte
		if _, err := io.ReadFull(r, pixel[:]); err != nil {
			return unexpectedEOF(err)
		}
		if pixel[0] == 1 && pixel[1] == 1 && pixel[2] == 1 {
			if x == 0 {
				return errors.New("run with no pixel to repeat")
			}
			run := int(pixel[3]) << shift
			if x+run > width {
				return errors.New("run past the end of the row")
			}
			previous := scanline[(x-1)*4 : x*4]
			for range run {
				copy(scanline[x*4:], previous)
				x++
			}
			shift += 8
			continue
		}
		copy(scanline[x*4:], pixel[:])
		x++
		shift = 0
	}
	return nil
}

// unexpectedEOF reports an end of file inside the pixels as io.ErrUnexpectedEOF.
func unexpectedEOF(err error) error {
	if err == io.EOF {
		return io.ErrUnexpectedEOF
	}
	return err
}

// decodeScanline converts a row of RGBE pixels to linear float RGB in row.
func decodeScanline(scanline []byte, row []float32) {
	for x := range len(scanline) / 4 {
		pixel := scanline[x*4 : x*4+4]
		if pixel[3] == 0 {
			// Black; row is already zero.
			continue
		}
		scale := exponentScales[pixel[3]]
		row[x*3] = (float32(pixel[0]) + 0.5) * scale
		row[x*3+1] = (float32(pixel[1]) + 0.5) * scale
		row[x*3+2] = (float32(pixel[2]) + 0.5) * scale
	}
}

// exponentScales is what a mantissa is multiplied by under each exponent: 2^(e-128)
// for a mantissa read as a fraction of 256. The half added to each mantissa above
// takes the middle of the interval it quantized, as Radiance's own reader does.
var exponentScales = func() (scales [256]float32) {
	for e := range scales {
		scales[e] = float32(math.Ldexp(1, e-128-8))
	}
	return scales
}()
