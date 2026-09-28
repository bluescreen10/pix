package postprocess

import (
	"github.com/bluescreen10/gamekit/gpu"
	"github.com/bluescreen10/gamekit/utils"
	"github.com/bluescreen10/pix/shaders"
)

// Halftone redraws the image the way a printing press would: as screens of ink dots on
// white paper, each dot sized for the tone beneath it. The zero value is a colour comic
// print — cyan, magenta, yellow and black screens at the classic print angles, with
// dots 8 pixels apart.
//
// Its output is paper and ink, all within white, so put it after the effects that
// need the scene's light above white (Bloom), and pair it with pix.ToneMapNone: a tone
// curve would grey the paper.
type Halftone struct {
	Style HalftoneStyle
	// CellSize is the distance between neighbouring dots, in pixels. Zero uses 8.
	CellSize float32
	// Angle rotates every screen, in radians. Zero keeps the classic angles.
	Angle float32

	pass *FullscreenPass
}

// HalftoneStyle is which screens Halftone prints.
type HalftoneStyle uint32

const (
	// HalftoneCMYK prints four screens — cyan, magenta, yellow and black — each at its
	// own angle, which is what makes print's rosette pattern.
	HalftoneCMYK HalftoneStyle = iota
	// HalftoneMono prints one black screen, for the image's brightness alone.
	HalftoneMono
)

// defaultHalftoneCellSize is the dot spacing when CellSize is zero, in pixels.
const defaultHalftoneCellSize = 8

// halftoneRoot is the root of shaders/src/halftone.frag.glsl.
type halftoneRoot struct {
	Root
	cellSize float32
	angle    float32
	style    HalftoneStyle
	_        float32
}

func (h *Halftone) Encode(frame *Frame, cmd gpu.CommandBuffer) {
	if h.pass == nil {
		h.pass = NewFullscreenPass(frame.Backend, FullscreenPassDescriptor{Fragment: shaders.Halftone, Label: "halftone"})
	}
	cellSize := h.CellSize
	if cellSize <= 0 {
		cellSize = defaultHalftoneCellSize
	}

	root := halftoneRoot{
		Root:     frame.Root(frame.Source, frame.Width, frame.Height),
		cellSize: cellSize,
		angle:    h.Angle,
		style:    h.Style,
	}
	h.pass.Draw(frame.Target, frame.Width, frame.Height, gpu.LoadClear, utils.ToBytes(&root), cmd)
}

func (h *Halftone) Release() {
	if h.pass != nil {
		h.pass.Release()
		h.pass = nil
	}
}
