package pix_test

import (
	"testing"

	"github.com/bluescreen10/pix"
	"github.com/bluescreen10/pix/colors"
	"github.com/bluescreen10/pix/glm"
	"github.com/bluescreen10/pix/scenes"
)

// TestShowFPS renders a few frames with the HUD enabled and checks the overlay text
// is visible (font-colored pixels) and that GPU timestamps produced a positive time.
func TestShowFPS(t *testing.T) {
	r, err := pix.NewOffscreenRenderer(320, 140)
	if err != nil {
		t.Fatal(err)
	}
	defer r.Destroy()
	r.SetClearColor([4]float32{0, 0, 0, 1})
	r.SetFontColor(colors.RGBA32F{1, 0.787, 0.1, 1}) // linear; the sRGB target stores (255,229,89)
	r.ShowFPS(true)

	scene := scenes.New()
	defer scene.Destroy()
	cam := scene.NewPerspectiveCamera(45, 1, 0.1, 1000)
	cam.SetPosition(glm.Vec3f{0, 0, 3})

	// Comfortably more frames than the stats' GPU warm-up discards (see profileWarmup), with
	// room on top: the first frames of a fresh backend carry pipeline compilation and
	// need not produce a usable timestamp at all, so a count that only just clears the
	// warm-up passes on one backend and fails on another.
	for i := 0; i < 12; i++ {
		r.Render(scene)
	}

	// Font pixels are the solid font color (255,229,89); count them.
	px := r.Pixels()
	lit := 0
	for i := 0; i < len(px); i += 4 {
		if px[i] > 200 && px[i+1] > 180 && px[i+2] < 160 {
			lit++
		}
	}
	if lit < 50 {
		t.Fatalf("overlay text not visible (%d font px)", lit)
	}
	if r.Profiler().GPUTime() <= 0 {
		t.Fatalf("no GPU time recorded via timestamps")
	}
	t.Logf("overlay lit=%d px | FPS=%.0f CPU=%.3fms GPU=%.3fms",
		lit, r.Profiler().FPS(),
		float64(r.Profiler().CPUTime().Microseconds())/1000,
		float64(r.Profiler().GPUTime().Microseconds())/1000)
}
