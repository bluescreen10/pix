package pix

import (
	"math"
	"unsafe"

	"github.com/bluescreen10/pix/colors"
	"github.com/bluescreen10/pix/glm"
	"github.com/bluescreen10/pix/gpu"
	"github.com/bluescreen10/pix/shaders"
	"github.com/bluescreen10/pix/textures"
)

// overlayQuad is one screen-space rectangle (scalar, 48 bytes); matches Quad in
// overlay.vert. rect = x,y (top-left px), z,w (w,h px); uv = u0,v0,u1,v1 into the
// glyph atlas.
type overlayQuad struct {
	rect  glm.Vec4f
	uv    glm.Vec4f
	color colors.RGBA32F
}

// overlayRoot matches Root in overlay.vert. Scalar layout: vec2 viewport at 0..8, the
// two heap indices at 8..16, then quads (a 64-bit reference) at 16 — every field lands
// on its own alignment, so there is no interior padding.
type overlayRoot struct {
	viewport glm.Vec2f
	atlas    uint32
	sampler  uint32
	quads    uint64
}

// overlay renders the debug HUD and console as instanced screen-space quads over the
// frame: one instance per character, sampling the glyph atlas built from font.go (see
// font_atlas.go). Solid fills are the same quad pointing at the atlas's opaque cell, so
// one draw covers text and panels alike.
type overlay struct {
	backend gpu.Backend
	pipe    gpu.Pipeline
	atlas   *fontAtlas
	scale   float32 // framebuffer pixels per logical point
	quads   []overlayQuad
	quadBuf gpu.Buffer
	quadCap int
	rootBuf gpu.Buffer
}

func newOverlay(b gpu.Backend, texStore *textures.Store, scale float32, colorFormat, depthFormat gpu.Format) *overlay {
	o := &overlay{backend: b, atlas: buildFontAtlas(texStore), scale: scale}
	o.pipe = b.CreateGraphicsPipeline(gpu.PipelineDescriptor{
		VertexShader: shaders.OverlayVert, FragmentShader: shaders.OverlayFrag,
		Topology: gpu.TopologyTriangles, ColorFormats: []gpu.Format{colorFormat},
		DepthFormat: depthFormat, DepthTest: false, DepthWrite: false, CullMode: gpu.CullNone,
		// Alpha blending, for two things that both need it: a translucent panel behind
		// the console, and coverage-weighted glyph pixels when text is drawn below the
		// font's native size (see text).
		Blend: []gpu.BlendState{{
			Enable:  true,
			ColorOp: gpu.BlendFactorOp{Src: gpu.BlendSrcAlpha, Dst: gpu.BlendOneMinusSrcAlpha, Op: gpu.BlendAdd},
		}},
	})
	o.rootBuf = b.Alloc(uint64(unsafe.Sizeof(overlayRoot{})), gpu.MemoryHost, "overlay-root")
	return o
}

// reset clears the accumulated quads (call once per frame before text calls).
func (o *overlay) reset() { o.quads = o.quads[:0] }

// rect appends one solid screen-space rectangle (x,y top-left, w×h, in pixels). It
// points at the atlas's fully-opaque cell, so a fill and a glyph are the same quad and
// the shader needs no branch between them.
func (o *overlay) rect(x, y, w, h float32, color colors.RGBA32F) {
	u, v := o.atlas.solidU, o.atlas.solidV
	o.quads = append(o.quads, overlayQuad{
		rect:  glm.Vec4f{x, y, w, h},
		uv:    glm.Vec4f{u, v, u, v}, // degenerate: every corner samples the same texel
		color: color,
	})
}

// advance is how far the pen moves past one glyph. The font is proportional, so this
// is per-character and is the single source of truth for both text and measure —
// anything that estimated it instead would drift across a line.
func (o *overlay) advance(ch rune, scale float32) float32 {
	e, ok := o.atlas.lookup(ch)
	if !ok {
		return 8 * scale // spaces / unknown glyphs
	}
	return float32(e.width) * scale
}

// snapSize rounds a requested glyph height (in framebuffer pixels) to the nearest whole
// multiple of the font's authored cell.
//
// The font is pixel art: its design IS the pixel grid, so it is only truly crisp when
// one source pixel maps to a whole number of screen pixels. At 1.5x, half the columns
// double and half do not and the glyph goes lumpy; with a smoothing filter instead it
// just goes soft. Rounding to 1x, 2x, 3x is what keeps it looking drawn rather than
// resampled — the same reason retro UIs scale in integers.
//
// It rounds DOWN, so text is never larger than asked for: oversized text breaks a
// layout, undersized text is merely small. Rounding to nearest instead would make a
// request of 1.5 cells jump up to 2, which on a 2x display silently doubles the
// console.
//
// The cost is that sizes are quantized, which is inherent to a bitmap font: more sizes
// means authoring more of them, as the original Mac did (Chicago 12, Geneva 9/10/12).
// One cell is the floor — there is nothing smaller to draw with.
func (o *overlay) snapSize(px float32) float32 {
	n := float32(math.Floor(float64(px / glyphCell)))
	if n < 1 {
		n = 1
	}
	return n * glyphCell
}

// measure returns the width in pixels of s rendered at glyph height `size`.
func (o *overlay) measure(s string, size float32) float32 {
	scale := o.snapSize(size) / glyphCell
	var w float32
	for _, ch := range s {
		w += o.advance(ch, scale)
	}
	return w
}

// text rasterizes s at (x,y) top-left in pixels, with glyph height `size` px: one
// textured quad per character, sampling the glyph atlas.
//
// Filtering is the sampler's job. That is the whole point of the atlas — drawing a
// glyph below its authored 16px used to mean emitting a sub-pixel quad per lit bit,
// which the rasterizer dropped wherever the quad missed a pixel centre, and small text
// came apart. Here the hardware filters between texels instead, and one glyph costs one
// primitive at any size.
func (o *overlay) text(s string, x, y, size float32, color colors.RGBA32F) {
	size = o.snapSize(size)
	scale := size / glyphCell
	cx := x
	for _, ch := range s {
		e, ok := o.atlas.lookup(ch)
		if !ok {
			cx += o.advance(ch, scale) // spaces / unknown glyphs
			continue
		}
		// The quad is a whole cell wide; the pen advances by the glyph's own width, so
		// a glyph that overhangs its advance still draws in full (see buildFontAtlas).
		o.quads = append(o.quads, overlayQuad{
			rect:  glm.Vec4f{cx, y, glyphCell * scale, size},
			uv:    glm.Vec4f{e.u0, e.v0, e.u1, e.v1},
			color: color,
		})
		cx += float32(e.width) * scale
	}
}

// draw records the overlay draw into cl for a viewport of vpW×vpH pixels.
func (o *overlay) draw(cmd gpu.CommandBuffer, vpW, vpH float32) {
	n := len(o.quads)
	if n == 0 {
		return
	}
	if n > o.quadCap {
		if o.quadBuf.Valid() {
			o.backend.Free(o.quadBuf)
		}
		o.quadCap = n * 2
		o.quadBuf = o.backend.Alloc(uint64(o.quadCap)*uint64(unsafe.Sizeof(overlayQuad{})), gpu.MemoryHost, "overlay-quads")
	}
	copy(unsafe.Slice((*overlayQuad)(o.quadBuf.Ptr), n), o.quads)
	*(*overlayRoot)(o.rootBuf.Ptr) = overlayRoot{
		viewport: glm.Vec2f{vpW, vpH},
		atlas:    o.atlas.tex.Index(),
		sampler:  o.atlas.sampler,
		quads:    o.quadBuf.Addr,
	}

	cmd.SetPipeline(o.pipe)
	cmd.Root(o.rootBuf.Addr)
	cmd.Draw(6, uint32(n), 0, 0)
}

func (o *overlay) destroy() {
	if o.atlas != nil {
		o.atlas.destroy()
		o.atlas = nil
	}
	if o.quadBuf.Valid() {
		o.backend.Free(o.quadBuf)
	}
	if o.rootBuf.Valid() {
		o.backend.Free(o.rootBuf)
	}
	o.backend.DestroyPipeline(o.pipe)
}
