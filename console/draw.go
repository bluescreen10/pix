package console

import "github.com/bluescreen10/pix/colors"

// Painter is the drawing surface a console paints onto: filled rectangles for the
// backdrop and cursor, a text run per line, and the width of a string so the cursor
// can be placed. Declared here, by the consumer, so this package takes no dependency
// on a renderer — pix's debug overlay satisfies it, and so can anything else that can
// put a rectangle and a string on screen.
//
// Measure is part of the interface because the painter owns the font: pix's overlay
// font is proportional (glyphs are 6 to 16 px wide at size 16), so anything that
// estimated an advance here would drift a few pixels per character and put the cursor
// under the wrong glyph.
//
// Coordinates are pixels from the top-left of the viewport.
type Painter interface {
	Rect(x, y, w, h float32, c colors.RGBA32F)
	Text(s string, x, y, size float32, c colors.RGBA32F)
	Measure(s string, size float32) float32

	// Scale is device pixels per logical point — 2 on a HiDPI display. The console
	// sizes itself in logical points so it stays the same physical size anywhere.
	Scale() float32

	// SnapSize maps a requested device-pixel glyph height to the one the painter will
	// actually draw at. pix's font is pixel art and is only crisp at whole multiples
	// of its authored cell, so it rounds; a painter with a scalable font would return
	// the size unchanged. The console asks rather than assuming, so its line spacing
	// matches the text that gets drawn instead of the size it wished for.
	SnapSize(px float32) float32
}

const prompt = "> "

// Draw paints the console over a vpW×vpH viewport. It is a no-op while hidden, so a
// caller can call it unconditionally.
//
// The layout is bottom-anchored: the prompt sits on the last line and the scrollback
// fills upward from it, so the newest output is always in the same place no matter how
// much has been printed.
func (c *Console) Draw(p Painter, vpW, vpH float32) {
	if !c.visible {
		return
	}
	// Everything the console holds is in logical points; convert once, here. The glyph
	// height goes through the painter's own rounding so the line step below matches the
	// text that actually lands on screen.
	dpi := p.Scale()
	size := p.SnapSize(c.fontSize * dpi)
	gap := size * lineSpacing
	pad := c.Padding * dpi

	h := vpH * c.Height
	p.Rect(0, 0, vpW, h, c.Background)
	p.Rect(0, h-2*dpi, vpW, 2*dpi, c.Border) // a rule along the bottom edge, so it reads as a panel

	// Prompt, with a caret drawn at the insertion point. It goes down before the text
	// so a caret sitting on a glyph does not hide it.
	promptY := h - pad - size
	line := prompt + string(c.edit)
	caretX := pad + p.Measure(prompt+string(c.edit[:c.cursor]), size)
	p.Rect(caretX, promptY, 2*dpi, size, c.CursorFG)
	p.Text(line, pad, promptY, size, c.PromptFG)

	// Scrollback above it, newest first walking upward, stopping once a line would be
	// off the top. c.scroll pages backward through the history.
	y := promptY - gap
	end := min(len(c.lines)-c.scroll, len(c.lines))
	for i := end - 1; i >= 0 && y > -gap; i-- {
		p.Text(c.lines[i], pad, y, size, c.Foreground)
		y -= gap
	}
}
