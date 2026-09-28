package postprocess_test

import (
	"testing"

	"github.com/bluescreen10/pix/colors"
	"github.com/bluescreen10/pix/postprocess"
)

// TestBloomSpreadsBrightLight: bloom carries light from a bright surface onto pixels
// around it that no geometry covers. Without it those pixels are the black background.
func TestBloomSpreadsBrightLight(t *testing.T) {
	r, scene, cam := postScene(t, colors.RGBA32F{50, 50, 50, 1}, 0.2)
	r.EnableHDR(true)
	// Well outside the quad, which spans the middle quarter of the frame.
	x, y := postSize/2+postSize/4, postSize/2

	r.Render(scene, cam)
	if dark := pixelAt(r, x, y); dark != [3]byte{} {
		t.Fatalf("without bloom the pixel beside the quad = %v, want black", dark)
	}

	r.AddPostProcessingStep(&postprocess.Bloom{Intensity: 0.1})
	r.Render(scene, cam)
	if glow := pixelAt(r, x, y); glow == [3]byte{} {
		t.Error("with bloom the pixel beside the quad is still black, want the quad's light spread onto it")
	}
}
