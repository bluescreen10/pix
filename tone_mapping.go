package pix

import "github.com/bluescreen10/pix/postprocess"

// ToneMapOperator is the curve an HDR frame's light is mapped through into the range a
// display can show (see Renderer.SetToneMapping). Every curve but ToneMapNone compresses
// highlights instead of clipping them.
type ToneMapOperator uint32

const (
	// ToneMapNeutral is Khronos PBR Neutral: it leaves colours below its compression
	// point almost untouched, so a material's base colour survives to the display, and
	// rolls highlights off towards white.
	ToneMapNeutral ToneMapOperator = iota
	// ToneMapACESFitted is Stephen Hill's fit of the ACES reference rendering and output
	// transforms for an sRGB display: filmic contrast and slightly desaturated brights.
	// It is a fit of the look, not a versioned ACES transform.
	ToneMapACESFitted
	// ToneMapReinhard is x/(1+x): simple, and keeps hues, but flattens contrast.
	ToneMapReinhard
	// ToneMapNone clips everything brighter than white, as a frame without HDR does.
	ToneMapNone
)

// toneMapRoot is the root of the tone-map pass, shaders/src/tonemap.frag.glsl.
type toneMapRoot struct {
	postprocess.Root
	operator ToneMapOperator
	exposure float32 // in stops
	_        [2]float32
}
