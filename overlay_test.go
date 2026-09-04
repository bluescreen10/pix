package pix

import (
	"testing"

	"github.com/bluescreen10/pix/cameras"
	"github.com/bluescreen10/pix/colors"
	"github.com/bluescreen10/pix/glm"
)

// TestShowFPS renders a few frames with the HUD enabled and checks the overlay text
// is visible (font-colored pixels) and that GPU timestamps produced a positive time.
func TestShowFPS(t *testing.T) {
	r, err := NewOffscreenRenderer(320, 140)
	if err != nil {
		t.Fatal(err)
	}
	defer r.Destroy()
	r.SetClearColor([4]float32{0, 0, 0, 1})
	r.fontColor = colors.RGBA32F{1, 0.9, 0.35, 1}
	r.ShowFPS(true)

	scene := r.NewScene()
	defer scene.Destroy()
	cam := cameras.NewPerspectiveCamera(45, 1, 0.1, 1000)
	cam.SetPosition(glm.Vec3f{0, 0, 3})

	for i := 0; i < 6; i++ {
		r.Render(scene, cam)
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
	if r.stats.AvgGPUTime() <= 0 {
		t.Fatalf("no GPU time recorded via timestamps")
	}
	t.Logf("overlay lit=%d px | FPS=%.0f CPU=%.3fms GPU=%.3fms",
		lit, r.stats.FPS(),
		float64(r.stats.AvgCPUTime().Microseconds())/1000,
		float64(r.stats.AvgGPUTime().Microseconds())/1000)
}
