package gltf

import (
	"image"
	"image/color"
	"image/png"
	"os"
	"testing"

	"github.com/bluescreen10/pix"
	"github.com/bluescreen10/pix/cameras"
	"github.com/bluescreen10/pix/glm"
)

// TestCaptureAnimatedFrames renders the capoeira actor at three points in its
// animation loop and saves them as PNGs — a visual (not just numeric) sanity
// check that loading + mixer + compute skinning produce a moving, recognizable
// figure, for a run where a live window can't be inspected directly.
func TestCaptureAnimatedFrames(t *testing.T) {
	if os.Getenv("PIX_CAPTURE_FRAMES") == "" {
		t.Skip("set PIX_CAPTURE_FRAMES=1 to run (writes PNGs to /tmp)")
	}
	r, err := pix.NewOffscreenRenderer(480, 480)
	if err != nil {
		t.Fatal(err)
	}
	defer r.Destroy()
	r.EnableShadows(true)
	r.EnableDeferredRendering(true)

	scene := r.NewScene()
	defer scene.Destroy()
	res, err := LoadFull(r, scene, capoeiraAsset)
	if err != nil {
		t.Fatal(err)
	}
	scene.SetAmbient(glm.Vec3f{0.35, 0.35, 0.42})
	light := scene.AddDirectionalLight(glm.Vec3f{-0.5, -1, -0.35}, glm.Vec3f{1, 0.96, 0.9}, 2.0)
	light.SetCastShadow(true)

	mixer := scene.NewAnimationMixer(res.Skeletons[0])
	action := mixer.Action(res.Clips[0])
	action.SetLoop(pix.LoopRepeat).Play()

	cam := cameras.NewPerspectiveCamera(45, 1, 0.01, 100)
	r.Render(scene, cam)
	center, radius := scene.FrameSphere(0.9)
	cam.SetPosition(center.Add(glm.Vec3f{0, 0, radius * 2.6}))
	cam.LookAt(center)
	cam.SetNear(radius * 0.02)
	cam.SetFar(radius * 24)

	for i, t0 := range []float32{0, 1.2, 2.4} {
		action.SetTime(t0)
		mixer.Update(0) // apply the jumped time without advancing further
		r.Render(scene, cam)
		pixels := r.Capture()
		savePNG(t, pixels, 480, 480, "/tmp/skinning_frame_"+string(rune('0'+i))+".png")
	}
}

func savePNG(t *testing.T, pixels []byte, w, h int, path string) {
	t.Helper()
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			i := (y*w + x) * 4
			img.SetRGBA(x, y, color.RGBA{pixels[i], pixels[i+1], pixels[i+2], 255})
		}
	}
	f, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	if err := png.Encode(f, img); err != nil {
		t.Fatal(err)
	}
	t.Logf("wrote %s", path)
}
