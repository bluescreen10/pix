package hdr

import (
	"image"
	"image/color"
	"math"
)

// RGB is an image of linear light: red, green and blue as float32, unbounded above — a
// sky's sun is thousands of times brighter than white.
type RGB struct {
	// Pix holds the pixels, three floats each, row by row. The pixel at (x, y) starts at
	// Pix[(y-Rect.Min.Y)*Stride + (x-Rect.Min.X)*3].
	Pix []float32
	// Stride is the distance in floats between vertically adjacent pixels.
	Stride int
	Rect   image.Rectangle
}

// NewRGB returns a black RGB image with the given bounds.
func NewRGB(bounds image.Rectangle) *RGB {
	return &RGB{
		Pix:    make([]float32, bounds.Dx()*bounds.Dy()*3),
		Stride: bounds.Dx() * 3,
		Rect:   bounds,
	}
}

// ColorModel returns ColorModel.
func (p *RGB) ColorModel() color.Model {
	return ColorModel
}

// Bounds returns the image's bounds.
func (p *RGB) Bounds() image.Rectangle {
	return p.Rect
}

// At returns the pixel at (x, y) as a Color.
func (p *RGB) At(x, y int) color.Color {
	return p.RGBAt(x, y)
}

// RGBAt returns the pixel at (x, y), or black outside the image.
func (p *RGB) RGBAt(x, y int) Color {
	if !(image.Point{X: x, Y: y}.In(p.Rect)) {
		return Color{}
	}
	i := p.PixOffset(x, y)
	return Color{R: p.Pix[i], G: p.Pix[i+1], B: p.Pix[i+2]}
}

// PixOffset returns the index in Pix of the first float of the pixel at (x, y).
func (p *RGB) PixOffset(x, y int) int {
	return (y-p.Rect.Min.Y)*p.Stride + (x-p.Rect.Min.X)*3
}

// Color is linear light, unbounded above.
type Color struct {
	R, G, B float32
}

// RGBA returns the color clipped to white and encoded as sRGB, which is how 8- and
// 16-bit images hold colour: drawing an HDR image into an image.RGBA gives a viewable,
// if clipped, picture rather than a dark one.
func (c Color) RGBA() (r, g, b, a uint32) {
	return encodeSRGB16(c.R), encodeSRGB16(c.G), encodeSRGB16(c.B), 0xffff
}

// ColorModel converts any color to a Color, decoding it from sRGB and dropping alpha.
var ColorModel = color.ModelFunc(func(c color.Color) color.Color {
	if c, ok := c.(Color); ok {
		return c
	}
	r, g, b, _ := c.RGBA()
	return Color{R: decodeSRGB16(r), G: decodeSRGB16(g), B: decodeSRGB16(b)}
})

// encodeSRGB16 encodes linear v, clipped to [0, 1], as a 16-bit sRGB value.
func encodeSRGB16(v float32) uint32 {
	l := math.Min(math.Max(float64(v), 0), 1)
	if l <= 0.0031308 {
		l *= 12.92
	} else {
		l = 1.055*math.Pow(l, 1/2.4) - 0.055
	}
	return uint32(l*0xffff + 0.5)
}

// decodeSRGB16 decodes a 16-bit sRGB value to linear.
func decodeSRGB16(v uint32) float32 {
	s := float64(v) / 0xffff
	if s <= 0.04045 {
		return float32(s / 12.92)
	}
	return float32(math.Pow((s+0.055)/1.055, 2.4))
}
