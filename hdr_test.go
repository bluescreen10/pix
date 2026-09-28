package pix_test

import (
	"fmt"
	"math"
	"testing"

	"github.com/bluescreen10/gamekit/gpu"
	"github.com/bluescreen10/pix"
	"github.com/bluescreen10/pix/cameras"
	"github.com/bluescreen10/pix/colors"
	"github.com/bluescreen10/pix/geometries"
	"github.com/bluescreen10/pix/glm"
	"github.com/bluescreen10/pix/postprocess"
	"github.com/bluescreen10/pix/scenes"
	"github.com/bluescreen10/pix/shaders"
)

const postSize = 96

// postScene renders one unlit quad of the given colour, covering the middle of the frame
// by the given fraction, over a black background. Unlit means the colour reaches the
// scene image as-is, so a colour above 1.0 is light above white.
func postScene(t *testing.T, color colors.RGBA32F, coverage float32) (*pix.Renderer, *scenes.Scene, pix.Camera) {
	t.Helper()
	r, err := pix.NewOffscreenRenderer(postSize, postSize)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(r.Destroy)
	r.SetClearColor(colors.RGBA32F{0, 0, 0, 1})

	scene := scenes.New()
	t.Cleanup(scene.Destroy)
	h := coverage
	quad := r.GeometryStore.Create(geometries.GeometryConfig{
		Attributes: []geometries.Attribute{
			geometries.NewAttribute(geometries.AttributePosition, geometries.Float32x3, []glm.Vec3f{{-h, -h, 0}, {h, -h, 0}, {h, h, 0}, {-h, h, 0}}),
		},
		Indices: []uint32{0, 1, 2, 0, 2, 3},
	})
	material := r.NewBasicMaterial()
	material.SetColor(color)
	scene.Add(scene.NewMesh(quad, material))

	// From 2 units away a 45-degree camera sees about ±0.83 of the plane z = 0, so a
	// coverage of 0.8 fills nearly the frame and 0.2 the middle quarter of it.
	cam := cameras.NewPerspectiveCamera(45, 1, 0.1, 100)
	cam.SetPosition(glm.Vec3f{0, 0, 2})
	return r, scene, cam
}

func pixelAt(r *pix.Renderer, x, y int) [3]byte {
	px := r.Pixels()
	i := (y*postSize + x) * 4
	return [3]byte{px[i], px[i+1], px[i+2]}
}

// srgbByte is the byte an 8-bit display target holds for linear light v.
func srgbByte(v float32) int {
	v = min(max(v, 0), 1)
	encoded := 12.92 * float64(v)
	if v > 0.0031308 {
		encoded = 1.055*math.Pow(float64(v), 1/2.4) - 0.055
	}
	return int(math.Round(encoded * 255))
}

// TestClearColourIsLinearLight: the clear colour is linear light like every other colour
// the renderer is given, so the background is its sRGB encoding — with HDR off, where it
// is written into the target directly, and with HDR on, where it passes through the
// scene image and the tone-map pass.
func TestClearColourIsLinearLight(t *testing.T) {
	clear := colors.RGBA32F{0.2, 0.5, 0.8, 1}
	for _, hdr := range []bool{false, true} {
		t.Run(fmt.Sprintf("HDR %v", hdr), func(t *testing.T) {
			r, scene, cam := postScene(t, colors.RGBA32F{0, 0, 0, 0}, 0)
			r.EnableHDR(hdr)
			r.SetToneMapping(pix.ToneMapNone) // so HDR changes nothing about light below white
			r.SetClearColor(clear)
			r.Render(scene, cam)

			got := pixelAt(r, 2, 2)
			for i, v := range []float32{clear[0], clear[1], clear[2]} {
				if d := int(got[i]) - srgbByte(v); d < -1 || d > 1 {
					t.Fatalf("background = %v, want the clear colour %v sRGB-encoded", got, clear)
				}
			}
		})
	}
}

// TestHDRKeepsLightAboveWhite: without HDR, light above 1.0 clips — an orange brighter
// than white turns yellow-white, because red and green both saturate. With HDR it is
// tone-mapped instead: the highlight is compressed, and its hue survives.
func TestHDRKeepsLightAboveWhite(t *testing.T) {
	orange := colors.RGBA32F{4, 1.5, 0.3, 1}

	r, scene, cam := postScene(t, orange, 0.8)
	r.Render(scene, cam)
	clipped := pixelAt(r, postSize/2, postSize/2)
	if clipped[0] != 255 || clipped[1] != 255 {
		t.Fatalf("without HDR the centre = %v, want red and green clipped to 255", clipped)
	}

	r.EnableHDR(true)
	r.Render(scene, cam)
	mapped := pixelAt(r, postSize/2, postSize/2)
	if mapped[0] == 255 || mapped[1] == 255 {
		t.Errorf("with HDR the centre = %v, want the highlight compressed below 255", mapped)
	}
	if !(mapped[0] > mapped[1] && mapped[1] > mapped[2]) {
		t.Errorf("with HDR the centre = %v, want the orange's hue kept: red > green > blue", mapped)
	}
}

// TestExposureScalesLight: each stop of exposure doubles the light before tone mapping,
// so a surface of 0.25 at +1 stop draws exactly as one of 0.5 at none.
func TestExposureScalesLight(t *testing.T) {
	r, scene, cam := postScene(t, colors.RGBA32F{0.25, 0.25, 0.25, 1}, 0.8)
	r.EnableHDR(true)
	r.SetToneMapping(pix.ToneMapNone)
	r.SetExposure(1)
	r.Render(scene, cam)

	got := pixelAt(r, postSize/2, postSize/2)
	if d := int(got[0]) - srgbByte(0.5); d < -1 || d > 1 {
		t.Errorf("centre = %v at +1 stop, want %d: 0.25 doubled to 0.5", got, srgbByte(0.5))
	}
}

// TestPostProcessingNeedsHDR: without HDR there is no scene image for the chain to work
// on, so it does not run — the frame costs nothing more for having one.
func TestPostProcessingNeedsHDR(t *testing.T) {
	r, scene, cam := postScene(t, colors.RGBA32F{50, 50, 50, 1}, 0.2)
	r.AddPostProcessingStep(&postprocess.Bloom{Intensity: 0.1})
	r.Render(scene, cam)
	if dark := pixelAt(r, postSize/2+postSize/4, postSize/2); dark != [3]byte{} {
		t.Errorf("without HDR the pixel beside the quad = %v, want black: the chain should not run", dark)
	}
}

// countingStep runs an application effect — bloom's downsampling shader as a small blur —
// and counts what the renderer asks of it.
type countingStep struct {
	postprocess.ShaderStep
	encodes, releases int
}

func (s *countingStep) Encode(frame *postprocess.Frame, cmd gpu.CommandBuffer) {
	s.encodes++
	s.ShaderStep.Encode(frame, cmd)
}

func (s *countingStep) Release() {
	s.releases++
	s.ShaderStep.Release()
}

// TestStepLeavingTheChainIsReleased: a step owns what it draws with, so the renderer must
// tell it when it is no longer run — and only then, not while it stays in the chain.
func TestStepLeavingTheChainIsReleased(t *testing.T) {
	r, scene, cam := postScene(t, colors.RGBA32F{1, 1, 1, 1}, 0.5)
	r.EnableHDR(true)
	kept := &countingStep{ShaderStep: postprocess.ShaderStep{Fragment: shaders.BloomDownsample, Params: make([]byte, 4)}}
	dropped := &countingStep{ShaderStep: postprocess.ShaderStep{Fragment: shaders.BloomDownsample, Params: make([]byte, 4)}}

	r.SetPostProcessing([]postprocess.Step{kept, dropped})
	r.Render(scene, cam)
	if kept.encodes != 1 || dropped.encodes != 1 {
		t.Fatalf("encodes = %d, %d, want each step run once", kept.encodes, dropped.encodes)
	}

	r.SetPostProcessing([]postprocess.Step{kept})
	if dropped.releases != 1 {
		t.Errorf("the dropped step was released %d times, want once", dropped.releases)
	}
	if kept.releases != 0 {
		t.Errorf("the kept step was released %d times, want none while it stays in the chain", kept.releases)
	}
	r.Render(scene, cam)
	if kept.encodes != 2 || dropped.encodes != 1 {
		t.Errorf("encodes = %d, %d, want only the kept step run again", kept.encodes, dropped.encodes)
	}
}
