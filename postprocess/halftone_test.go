package postprocess_test

import (
	"testing"

	"github.com/bluescreen10/pix"
	"github.com/bluescreen10/pix/colors"
	"github.com/bluescreen10/pix/postprocess"
)

// printed renders a surface of the given colour filling the frame through a halftone,
// with no tone curve to grey the paper, and returns the middle of the frame's pixels.
func printed(t *testing.T, color colors.RGBA32F, halftone *postprocess.Halftone) [][3]byte {
	t.Helper()
	r, scene, cam := postScene(t, color, 1)
	r.EnableHDR(true)
	r.SetToneMapping(pix.ToneMapNone)
	r.SetPostProcessing([]postprocess.Step{halftone})
	r.Render(scene, cam)

	var pixels [][3]byte
	for y := postSize / 4; y < postSize*3/4; y++ {
		for x := postSize / 4; x < postSize*3/4; x++ {
			pixels = append(pixels, pixelAt(r, x, y))
		}
	}
	return pixels
}

// inkShare is the share of pixels that are ink rather than paper: darker than mid-grey.
func inkShare(pixels [][3]byte) float64 {
	ink := 0
	for _, p := range pixels {
		if int(p[0])+int(p[1])+int(p[2]) < 3*128 {
			ink++
		}
	}
	return float64(ink) / float64(len(pixels))
}

// TestHalftonePrintsDotsOnPaper: a flat grey is not printed as grey but as black dots
// on white paper — nearly every pixel one or the other, and plenty of each.
func TestHalftonePrintsDotsOnPaper(t *testing.T) {
	pixels := printed(t, colors.RGBA32F{0.2, 0.2, 0.2, 1}, &postprocess.Halftone{Style: postprocess.HalftoneMono})

	paper, ink := 0, 0
	for _, p := range pixels {
		switch {
		case p[0] > 235 && p[1] > 235 && p[2] > 235:
			paper++
		case p[0] < 20 && p[1] < 20 && p[2] < 20:
			ink++
		}
	}
	n := len(pixels)
	if paper < n/5 || ink < n/5 {
		t.Errorf("paper %d, ink %d of %d pixels, want at least a fifth of each", paper, ink, n)
	}
	if paper+ink < n*3/4 {
		t.Errorf("only %d of %d pixels are paper or ink, want at least three quarters: the rest are the dots' antialiased edges", paper+ink, n)
	}
}

// TestHalftoneInkFollowsTone: a darker surface prints bigger dots, so more of the page
// is ink.
func TestHalftoneInkFollowsTone(t *testing.T) {
	// Each print in a subtest of its own, so its renderer is gone before the next one
	// starts: on Vulkan two renderers share one device, and the first to be destroyed
	// takes it with it.
	var dark, light float64
	t.Run("dark", func(t *testing.T) {
		dark = inkShare(printed(t, colors.RGBA32F{0.05, 0.05, 0.05, 1}, &postprocess.Halftone{Style: postprocess.HalftoneMono}))
	})
	t.Run("light", func(t *testing.T) {
		light = inkShare(printed(t, colors.RGBA32F{0.5, 0.5, 0.5, 1}, &postprocess.Halftone{Style: postprocess.HalftoneMono}))
	})
	if dark <= light+0.2 {
		t.Errorf("ink covers %.2f of a dark surface and %.2f of a light one, want clearly more on the dark one", dark, light)
	}
}

// TestHalftoneSeparatesColour: red is printed with magenta and yellow ink alone. Neither
// absorbs red light, so every pixel keeps its red; and both screens show, each taking
// out its own complement — magenta the green, yellow the blue.
func TestHalftoneSeparatesColour(t *testing.T) {
	pixels := printed(t, colors.RGBA32F{1, 0, 0, 1}, &postprocess.Halftone{})

	magenta, yellow := 0, 0
	for _, p := range pixels {
		if p[0] < 235 {
			t.Fatalf("a pixel of the red print is %v, want its red kept: no cyan or black ink should print", p)
		}
		if p[1] < 20 {
			magenta++
		}
		if p[2] < 20 {
			yellow++
		}
	}
	if magenta < len(pixels)/5 || yellow < len(pixels)/5 {
		t.Errorf("magenta ink on %d and yellow on %d of %d pixels, want both screens to print", magenta, yellow, len(pixels))
	}
}
