package pix

import (
	"github.com/bluescreen10/pix/colors"
	"github.com/bluescreen10/pix/console"
)

// consolePainter adapts the debug overlay to console.Painter. The overlay's own
// methods stay unexported — the console asked for a drawing surface, not for the
// overlay's internals to become public API.
type consolePainter struct{ o *overlay }

var _ console.Painter = consolePainter{}

func (p consolePainter) Rect(x, y, w, h float32, c colors.RGBA32F) {
	p.o.rect(x, y, w, h, c)
}

func (p consolePainter) Text(s string, x, y, size float32, c colors.RGBA32F) {
	p.o.text(s, x, y, size, c)
}

func (p consolePainter) Measure(s string, size float32) float32 {
	return p.o.measure(s, size)
}

func (p consolePainter) Scale() float32 {
	return p.o.scale
}

func (p consolePainter) SnapSize(px float32) float32 {
	return p.o.snapSize(px)
}
