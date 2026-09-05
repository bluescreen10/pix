package pix

import (
	"testing"

	"github.com/bluescreen10/pix/colors"
	"github.com/bluescreen10/pix/glm"
)

// synthOverlay is an overlay with a hand-built atlas, so the quad-emission tests below
// need no GPU: they are about geometry and UVs, not about uploading a texture.
func synthOverlay() *overlay {
	return &overlay{atlas: &fontAtlas{
		solidU: 0.5, solidV: 0.5,
		entries: map[rune]atlasEntry{
			// u1-u0 spans a whole cell for both; only width (the pen step) differs.
			'A': {u0: 0, v0: 0, u1: 0.1, v1: 0.2, width: 12},
			'i': {u0: 0.1, v0: 0, u1: 0.2, v1: 0.2, width: 6},
		},
	}}
}

// TestTextEmitsOneQuadPerGlyph is the point of the atlas: a character costs one
// primitive at any size, where the old path emitted one per lit bitmap pixel (dozens)
// and fell apart below the font's authored size.
func TestTextEmitsOneQuadPerGlyph(t *testing.T) {
	for _, size := range []float32{8, 12, 16, 26} {
		o := synthOverlay()
		o.text("AiA", 0, 0, size, colors.RGBA32F{1, 1, 1, 1})
		if len(o.quads) != 3 {
			t.Fatalf("size %v emitted %d quads for 3 glyphs, want 3", size, len(o.quads))
		}
	}
}

// TestTextQuadsCarryGlyphUVsAndAdvance: each quad must cover its glyph's own advance
// width and point at that glyph's cell, or the text renders squashed or as the wrong
// character.
func TestTextQuadsCarryGlyphUVsAndAdvance(t *testing.T) {
	o := synthOverlay()
	const size = 16 // scale 1: advances are the font's own widths
	o.text("Ai", 10, 20, size, colors.RGBA32F{1, 1, 1, 1})

	a, i := o.quads[0], o.quads[1]
	// The quad is a whole cell wide (glyphs may overhang their advance); the PEN steps
	// by the advance, which is what puts 'i' at x=22 rather than x=16.
	if a.rect != (glmVec4(10, 20, glyphCell, size)) {
		t.Fatalf("first quad rect = %v, want a cell-wide box at {10 20}", a.rect)
	}
	if i.rect[0] != 22 {
		t.Fatalf("second quad x = %v, want 22 (after A's 12px advance)", i.rect[0])
	}
	if i.rect[2] != glyphCell {
		t.Fatalf("second quad width = %v, want a whole cell", i.rect[2])
	}
	if a.uv != (glmVec4(0, 0, 0.1, 0.2)) {
		t.Fatalf("first quad uv = %v, want A's cell", a.uv)
	}
	if i.uv != (glmVec4(0.1, 0, 0.2, 0.2)) {
		t.Fatalf("second quad uv = %v, want i's cell", i.uv)
	}
}

// TestTextScalesWithSize: at twice the authored size a glyph must be twice as wide and
// advance twice as far, while still costing one quad.
func TestTextScalesWithSize(t *testing.T) {
	o := synthOverlay()
	o.text("Ai", 0, 0, 32, colors.RGBA32F{1, 1, 1, 1}) // scale 2

	if got := o.quads[0].rect[2]; got != 2*glyphCell {
		t.Fatalf("A cell width at 2x = %v, want %v", got, 2*glyphCell)
	}
	if got := o.quads[1].rect[0]; got != 24 {
		t.Fatalf("i starts at %v, want 24", got)
	}
	if got := o.measure("Ai", 32); got != 36 { // (12 + 6) * 2
		t.Fatalf("measure = %v, want 36", got)
	}
}

// TestSizesSnapToWholeCells pins the rule that keeps pixel art crisp: a requested
// height is rounded to a whole multiple of the authored 16px cell, so one source pixel
// always covers a whole number of screen pixels. Fractional scales are what make a
// scaled bitmap font look lumpy (nearest) or mushy (linear).
func TestSizesSnapToWholeCells(t *testing.T) {
	o := synthOverlay()
	for _, tc := range []struct{ ask, want float32 }{
		{4, 16},  // below one cell still draws at one cell — nothing smaller exists
		{12, 16}, // ditto
		{16, 16},
		{24, 16}, // 1.5x floors to 1x rather than jumping up to 2x
		{31, 16},
		{32, 32},
		{40, 32}, // 2.5x floors to 2x
		{48, 48},
	} {
		if got := o.snapSize(tc.ask); got != tc.want {
			t.Errorf("snapSize(%v) = %v, want %v", tc.ask, got, tc.want)
		}
	}

	// And what is drawn uses the snapped size, not the request.
	o.text("A", 0, 0, 24, colors.RGBA32F{1, 1, 1, 1})
	if got := o.quads[0].rect[3]; got != 16 {
		t.Fatalf("a 24px request drew at height %v, want the floored 16", got)
	}
	// measure must agree with the pen, or the console caret lands in the wrong place.
	if got := o.measure("A", 24); got != 12 {
		t.Fatalf("measure = %v, want A's 12px advance at the snapped size", got)
	}
}

// TestMeasureMatchesLaidOutText: the caret is placed with measure, so it must agree
// with where text actually put the pen — a mismatch drifts the cursor along the line.
//
// The pen is read from where each quad STARTS, not how wide it is: quads are a whole
// cell wide regardless of advance, so their width says nothing about the pen.
func TestMeasureMatchesLaidOutText(t *testing.T) {
	const str, size = "AiiA", float32(16)
	o := synthOverlay()
	o.text(str, 0, 0, size, colors.RGBA32F{1, 1, 1, 1})

	// Every prefix of the string must measure to where the next glyph was placed.
	runes := []rune(str)
	for i := 1; i < len(runes); i++ {
		want := o.quads[i].rect[0]
		if got := o.measure(string(runes[:i]), size); got != want {
			t.Fatalf("measure(%q) = %v but that glyph was drawn at x=%v", string(runes[:i]), got, want)
		}
	}
	// And the whole string: A(12) + i(6) + i(6) + A(12).
	if got := o.measure(str, size); got != 36 {
		t.Fatalf("measure(%q) = %v, want 36", str, got)
	}
}

// TestRectSamplesTheOpaqueCell: solid fills go through the same shader path as glyphs,
// so their UV rect must be the degenerate point at the atlas's opaque cell. Anything
// else and the console backdrop picks up a glyph's coverage and turns patchy.
func TestRectSamplesTheOpaqueCell(t *testing.T) {
	o := synthOverlay()
	o.rect(1, 2, 30, 40, colors.RGBA32F{0, 0, 0, 0.5})

	q := o.quads[0]
	if q.rect != (glmVec4(1, 2, 30, 40)) {
		t.Fatalf("rect = %v, want {1 2 30 40}", q.rect)
	}
	u, v := o.atlas.solidU, o.atlas.solidV
	if q.uv != (glmVec4(u, v, u, v)) {
		t.Fatalf("uv = %v, want the degenerate point (%v,%v)", q.uv, u, v)
	}
}

// TestUnknownRuneAdvancesWithoutDrawing: an unmapped character must not emit a quad
// (it would sample garbage) but must still move the pen, or the rest of the line
// shifts left.
func TestUnknownRuneAdvancesWithoutDrawing(t *testing.T) {
	o := synthOverlay()
	o.text("A☃A", 0, 0, 16, colors.RGBA32F{1, 1, 1, 1}) // snowman is not in the font

	if len(o.quads) != 2 {
		t.Fatalf("emitted %d quads, want 2 (the unknown rune draws nothing)", len(o.quads))
	}
	if got := o.quads[1].rect[0]; got != 20 { // 12 (A) + 8 (unknown fallback advance)
		t.Fatalf("second A starts at %v, want 20 — the unknown rune did not advance", got)
	}
}

// TestBuildFontAtlasPlacesEveryGlyph checks the bake itself against the real font: one
// entry per glyph, UVs inside the texture, and an opaque cell that does not overlap any
// of them.
func TestBuildFontAtlasPlacesEveryGlyph(t *testing.T) {
	r, err := NewOffscreenRenderer(32, 32)
	if err != nil {
		t.Fatal(err)
	}
	defer r.Destroy()

	a := buildFontAtlas(r.TextureStore)
	defer a.destroy()

	if len(a.entries) != len(font) {
		t.Fatalf("atlas holds %d glyphs, font has %d", len(a.entries), len(font))
	}
	if !a.tex.Valid() {
		t.Fatal("atlas texture was not created")
	}
	for ch, e := range a.entries {
		if e.u0 < 0 || e.v0 < 0 || e.u1 > 1 || e.v1 > 1 {
			t.Fatalf("glyph %q has UVs outside the atlas: %v", ch, e)
		}
		if e.u1 <= e.u0 || e.v1 <= e.v0 {
			t.Fatalf("glyph %q has a degenerate UV rect: %v", ch, e)
		}
		if e.width != font[int(ch)].width {
			t.Fatalf("glyph %q advance = %d, want %d", ch, e.width, font[int(ch)].width)
		}
		// The opaque cell must not fall inside a glyph's rect, or solid fills would
		// pick up that glyph's coverage.
		if a.solidU >= e.u0 && a.solidU < e.u1 && a.solidV >= e.v0 && a.solidV < e.v1 {
			t.Fatalf("the opaque cell sits inside glyph %q", ch)
		}
	}
}

// TestBuildFontAtlasIsDeterministic: map iteration order is random, so the layout has
// to be sorted or a glyph's cell moves between runs.
func TestBuildFontAtlasIsDeterministic(t *testing.T) {
	r, err := NewOffscreenRenderer(32, 32)
	if err != nil {
		t.Fatal(err)
	}
	defer r.Destroy()

	a := buildFontAtlas(r.TextureStore)
	defer a.destroy()
	b := buildFontAtlas(r.TextureStore)
	defer b.destroy()

	for ch, ea := range a.entries {
		if eb := b.entries[ch]; eb != ea {
			t.Fatalf("glyph %q landed at %v then %v — the layout is not stable", ch, ea, eb)
		}
	}
}

// glmVec4 is a terse literal for the expectations above.
func glmVec4(x, y, z, w float32) glm.Vec4f { return glm.Vec4f{x, y, z, w} }

// TestOverhangingGlyphsAreNotCropped: '<' and '>' are drawn 8 columns wide but advance
// only 6, so cropping each glyph's UV to its advance sliced the arrow tips off — the
// console prompt came out truncated. The cell is the drawn extent; the advance is only
// the pen step.
func TestOverhangingGlyphsAreNotCropped(t *testing.T) {
	r, err := NewOffscreenRenderer(32, 32)
	if err != nil {
		t.Fatal(err)
	}
	defer r.Destroy()

	a := buildFontAtlas(r.TextureStore)
	defer a.destroy()

	cellU := float32(glyphCell) / float32(atlasCols*(glyphCell+glyphPad))
	for _, ch := range []rune{'<', '>'} {
		e, ok := a.lookup(ch)
		if !ok {
			t.Fatalf("%q missing from the atlas", ch)
		}
		if got := e.u1 - e.u0; abs32(got-cellU) > 1e-6 {
			t.Errorf("%q spans %v of the atlas, want a whole cell (%v) — its ink overhangs its %dpx advance",
				ch, got, cellU, e.width)
		}
	}
}
