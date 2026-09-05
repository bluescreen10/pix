package pix

import (
	"sort"

	"github.com/bluescreen10/pix/gpu"
	"github.com/bluescreen10/pix/textures"
)

// The glyph atlas: the 1-bit bitmap in font.go baked into one texture so the overlay
// can draw a character as a single textured quad instead of one quad per lit pixel.
//
// It is generated at run time rather than checked in, so font.go stays the only place
// glyphs are authored — building it is a few hundred microseconds, once, when the
// overlay is created.

const (
	// glyphCell is the source glyph box in font units; the bitmap is 16 rows of 16 bits.
	glyphCell = 16
	// glyphPad separates cells so linear filtering (and the first mip) cannot pull ink
	// from a neighbouring glyph into the one being sampled.
	glyphPad = 2
	// atlasCols is how many cells sit in a row of the atlas.
	atlasCols = 16
)

// atlasEntry is where one glyph landed in the atlas, in normalized texture
// coordinates, plus the advance it had in the source font.
type atlasEntry struct {
	u0, v0, u1, v1 float32
	width          int
}

// fontAtlas is the baked texture plus the lookup from rune to its place in it.
type fontAtlas struct {
	tex     textures.Texture
	sampler uint32
	entries map[rune]atlasEntry

	// solid is a fully-opaque texel. Untextured quads (the console backdrop, the
	// caret) sample it, so every quad takes the same path through the shader and no
	// branch or sentinel is needed to tell solid fills from glyphs.
	solidU, solidV float32
}

// buildFontAtlas rasterizes every glyph in font.go into one R8 texture.
//
// Cells are laid out on a padded grid; one extra cell is filled solid for untextured
// quads. Source pixels are copied 1:1 — the bitmap is the resolution it is, and
// upscaling here would only invent detail — so the texture is binary, and smooth
// downscaling comes from the sampler's filtering and mip chain instead of from the CPU
// walking pixels the way the old per-pixel path did.
func buildFontAtlas(store *textures.Store) *fontAtlas {
	// Deterministic order: map iteration is random, and a glyph's cell must not move
	// between runs (it would make the atlas unreproducible when debugging).
	codes := make([]int, 0, len(font))
	for code := range font {
		codes = append(codes, code)
	}
	sort.Ints(codes)

	cell := glyphCell + glyphPad
	cells := len(codes) + 1 // +1 for the solid cell
	rows := (cells + atlasCols - 1) / atlasCols
	w, h := atlasCols*cell, rows*cell

	// RGBA8 in, R8 out: textures.Grayscale keeps the red channel and box-filters the
	// mip chain, which is exactly the minification this needs.
	px := make([]byte, w*h*4)
	set := func(x, y int, v byte) {
		i := (y*w + x) * 4
		px[i], px[i+1], px[i+2], px[i+3] = v, v, v, 255
	}

	a := &fontAtlas{entries: make(map[rune]atlasEntry, len(codes))}
	fw, fh := float32(w), float32(h)

	for i, code := range codes {
		g := font[code]
		cx, cy := (i%atlasCols)*cell, (i/atlasCols)*cell
		for row := 0; row < glyphCell && row < len(g.data); row++ {
			bits := uint16(g.data[row])
			for col := range glyphCell {
				if bits>>uint(col)&1 != 0 {
					set(cx+col, cy+row, 255)
				}
			}
		}
		// The UV rect covers the WHOLE cell, not just the advance width. A glyph may
		// legitimately overhang its advance — '<' and '>' are drawn 8 columns wide but
		// advance 6 — and cropping to the advance sliced their tips off. The quad is
		// cell-sized to match; the parts outside the ink are transparent, so glyphs
		// overlapping their neighbours costs nothing visually. Only the pen step uses
		// width.
		a.entries[rune(code)] = atlasEntry{
			u0:    float32(cx) / fw,
			v0:    float32(cy) / fh,
			u1:    float32(cx+glyphCell) / fw,
			v1:    float32(cy+glyphCell) / fh,
			width: g.width,
		}
	}

	// The solid cell, in the slot after the last glyph.
	sx, sy := (len(codes)%atlasCols)*cell, (len(codes)/atlasCols)*cell
	for y := range glyphCell {
		for x := range glyphCell {
			set(sx+x, sy+y, 255)
		}
	}
	// Sample its centre, far from the padding, so no mip level can dilute it.
	a.solidU = (float32(sx) + glyphCell/2) / fw
	a.solidV = (float32(sy) + glyphCell/2) / fh

	a.tex = store.Create(px, w, h, textures.Grayscale)
	a.sampler = store.CreateSampler(gpuAtlasSampler())
	return a
}

// lookup returns a glyph's atlas entry. Unknown runes fall back to a zero-width entry
// pointing at the solid cell's corner, which draws nothing.
func (a *fontAtlas) lookup(ch rune) (atlasEntry, bool) {
	e, ok := a.entries[ch]
	return e, ok
}

func (a *fontAtlas) destroy() {
	a.tex.Release()
}

// gpuAtlasSampler is nearest + clamped, deliberately.
//
// The font is hand-drawn pixel art, and the overlay only ever draws it at whole
// multiples of its authored cell (see overlay.snapSize), so every screen pixel lands
// squarely inside one source texel — point sampling reproduces the artwork exactly.
// Linear filtering would instead blend across texel edges and soften every stroke,
// which is precisely what makes a scaled bitmap font look mushy rather than drawn.
//
// Clamped so sampling at the edge of a cell cannot wrap to the far side of the atlas.
func gpuAtlasSampler() gpu.SamplerDescriptor {
	return gpu.SamplerDescriptor{
		MinLinear: false, MagLinear: false, MipLinear: false,
		AddressU: gpu.AddressClamp, AddressV: gpu.AddressClamp, AddressW: gpu.AddressClamp,
		Label: "font-atlas",
	}
}
