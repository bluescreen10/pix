package textures

import (
	"image"
	"image/draw"
)

// basePixels is img's pixels in format's 8-bit layout: a tightly packed *image.Gray's
// own bytes for Grayscale, else img as RGBA, narrowed to format's channels.
func basePixels(img image.Image, format Format) []byte {
	bounds := img.Bounds()
	w, h := bounds.Dx(), bounds.Dy()
	if gray, ok := img.(*image.Gray); ok && format == Grayscale && gray.Stride == w && bounds.Min == (image.Point{}) {
		return gray.Pix[:w*h]
	}
	return format.repack(nrgbaPixels(img), w, h)
}

// nrgbaPixels is img as tightly packed, non-premultiplied RGBA bytes. PNGs with alpha
// decode as NRGBA, which is that already, row for row. Opaque images — JPEG's YCbCr,
// opaque PNGs' RGBA and Gray — go through image/draw into RGBA, which it converts them
// to on fast paths; with alpha 1 everywhere, premultiplied is the same as not. Anything
// else takes image/draw's general path. Asking each pixel for its colour instead cost a
// conversion and an allocation apiece.
func nrgbaPixels(img image.Image) []byte {
	bounds := img.Bounds()
	w, h := bounds.Dx(), bounds.Dy()
	if nrgba, ok := img.(*image.NRGBA); ok && nrgba.Stride == w*4 && bounds.Min == (image.Point{}) {
		return nrgba.Pix[:w*h*4]
	}
	if isOpaque(img) {
		dst := image.NewRGBA(image.Rect(0, 0, w, h))
		draw.Draw(dst, dst.Bounds(), img, bounds.Min, draw.Src)
		return dst.Pix
	}
	dst := image.NewNRGBA(image.Rect(0, 0, w, h))
	draw.Draw(dst, dst.Bounds(), img, bounds.Min, draw.Src)
	return dst.Pix
}

// isOpaque reports whether img has no alpha below 1: always, for the types that hold
// none, and by looking, for RGBA.
func isOpaque(img image.Image) bool {
	switch img := img.(type) {
	case *image.YCbCr, *image.Gray:
		return true
	case *image.RGBA:
		return img.Opaque()
	}
	return false
}
