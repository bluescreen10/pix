package pix_test

import (
	"testing"

	"github.com/bluescreen10/pix"
	"github.com/bluescreen10/pix/colors"
	"github.com/bluescreen10/pix/geometries"
	"github.com/bluescreen10/pix/glm"
	"github.com/bluescreen10/pix/materials"
	"github.com/bluescreen10/pix/scenes"
)

// edgeScene is an unlit white quad over a black background, turned about the view axis
// so that its edges cross pixels at a slant.
func edgeScene(t *testing.T) (*pix.Renderer, *scenes.Scene) {
	t.Helper()
	r, err := pix.NewOffscreenRenderer(postSize, postSize)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(r.Destroy)
	r.SetClearColor(colors.RGBA32F{0, 0, 0, 1})

	scene := scenes.New()
	t.Cleanup(scene.Destroy)
	quad := r.GeometryStore.Create(geometries.GeometryConfig{
		Attributes: []geometries.Attribute{
			geometries.NewAttribute(geometries.AttributePosition, geometries.Float32x3, []glm.Vec3f{{-0.5, -0.5, 0}, {0.5, -0.5, 0}, {0.5, 0.5, 0}, {-0.5, 0.5, 0}}),
		},
		Indices: []uint32{0, 1, 2, 0, 2, 3},
	})
	white := r.NewBasicMaterial()
	white.SetColor(colors.RGBA32F{1, 1, 1, 1})
	mesh := scene.NewMesh(quad, white)
	mesh.SetRotationXYZ(0, 0, 0.35)
	scene.Add(mesh)

	cam := scene.NewPerspectiveCamera(45, 1, 0.1, 100)
	scene.Add(cam)
	cam.SetPosition(glm.Vec3f{0, 0, 2})
	return r, scene
}

// partlyCoveredPixels counts the pixels whose channel lies strictly between 0 and 255:
// in a scene of two flat colours, the pixels an edge crosses.
func partlyCoveredPixels(r *pix.Renderer, channel int) int {
	pixels := r.Pixels()
	count := 0
	for i := 0; i < len(pixels); i += 4 {
		if v := pixels[i+channel]; v != 0 && v != 255 {
			count++
		}
	}
	return count
}

// TestAntiAliasingSmoothsEdges: without anti-aliasing every pixel is either the quad's
// white or the background's black; with any method, pixels along the quad's edges blend
// the two, while the flat middle and the empty corner keep their colours exactly. With
// HDR on, MSAA resolves in linear light before tone mapping and FXAA runs after it.
func TestAntiAliasingSmoothsEdges(t *testing.T) {
	methods := []struct {
		name    string
		enabled bool
		method  pix.AntiAliasing
	}{
		{"off", false, pix.AntiAliasingMSAA4x},
		{"msaa2x", true, pix.AntiAliasingMSAA2x},
		{"msaa4x", true, pix.AntiAliasingMSAA4x},
		{"fxaa", true, pix.AntiAliasingFXAA},
	}
	for _, hdr := range []bool{false, true} {
		for _, m := range methods {
			t.Run(map[bool]string{false: "LDR ", true: "HDR "}[hdr]+m.name, func(t *testing.T) {
				r, scene := edgeScene(t)
				r.EnableHDR(hdr)
				r.SetToneMapping(pix.ToneMapNone)
				r.SetAntiAliasing(m.method)
				r.EnableAntiAliasing(m.enabled)
				r.Render(scene)

				partial := partlyCoveredPixels(r, 0)
				if !m.enabled && partial != 0 {
					t.Errorf("partly covered pixels = %d, want 0 without anti-aliasing", partial)
				}
				if m.enabled && partial == 0 {
					t.Error("partly covered pixels = 0, want the quad's edges blended with the background")
				}
				if got := pixelAt(r, postSize/2, postSize/2); got != [3]byte{255, 255, 255} {
					t.Errorf("centre = %v, want the quad's white untouched", got)
				}
				if got := pixelAt(r, 0, 0); got != [3]byte{} {
					t.Errorf("corner = %v, want the background's black untouched", got)
				}
			})
		}
	}
}

// TestAntiAliasingSceneSamples: the frame steps are told how many samples the scene is
// drawn with — more than one only for an MSAA method that is enabled — as the settings
// change between frames, and enabling anti-aliasing alone picks MSAA 4x.
func TestAntiAliasingSceneSamples(t *testing.T) {
	r, scene := stepScene(t)
	var log []string
	step := &recordingStep{name: "samples", log: &log}
	r.AddFrameStep(pix.FrameStageAfterOpaque, step)

	changes := []struct {
		name   string
		change func()
		want   uint8
	}{
		{"default", func() {}, 1},
		{"enabled", func() { r.EnableAntiAliasing(true) }, 4},
		{"msaa2x", func() { r.SetAntiAliasing(pix.AntiAliasingMSAA2x) }, 2},
		{"fxaa", func() { r.SetAntiAliasing(pix.AntiAliasingFXAA) }, 1},
		{"msaa4x", func() { r.SetAntiAliasing(pix.AntiAliasingMSAA4x) }, 4},
		{"disabled", func() { r.EnableAntiAliasing(false) }, 1},
	}
	for _, c := range changes {
		c.change()
		r.Render(scene)
		if got := step.frames[len(step.frames)-1].SceneSamples; got != c.want {
			t.Errorf("%s: SceneSamples = %d, want %d", c.name, got, c.want)
		}
	}
	if got := r.AntiAliasing(); got != pix.AntiAliasingMSAA4x {
		t.Errorf("AntiAliasing() = %v, want %v, the method last set", got, pix.AntiAliasingMSAA4x)
	}
}

// TestAntiAliasingNamesRoundTrip: every method's name parses back to it, and an unknown
// name does not parse.
func TestAntiAliasingNamesRoundTrip(t *testing.T) {
	for _, name := range pix.AntiAliasingNames() {
		method, ok := pix.ParseAntiAliasing(name)
		if !ok || method.String() != name {
			t.Errorf("ParseAntiAliasing(%q) = %v, %v; want the method named %q", name, method, ok, name)
		}
	}
	if _, ok := pix.ParseAntiAliasing("ssaa"); ok {
		t.Error(`ParseAntiAliasing("ssaa") reports ok, want an unknown name`)
	}
}

// TestMSAABackgroundFillsUncoveredSamples: an environment drawn behind the scene tests
// depth per sample, so in a pixel an edge crosses it fills exactly the samples the quad
// left uncovered. White and red share a full red channel, so every pixel's red is 255 —
// a pixel resolved with any of the black clear colour left in it would read less.
func TestMSAABackgroundFillsUncoveredSamples(t *testing.T) {
	r, scene := edgeScene(t)
	r.EnableAntiAliasing(true)
	red := environmentImage(r, 8, 4, uniform([3]byte{255, 0, 0}))
	environment := scenes.NewEnvironment(red)
	red.Release()
	defer environment.Release()
	environment.Background = true
	scene.SetEnvironment(environment)

	r.Render(scene)

	pixels := r.Pixels()
	for i := 0; i < len(pixels); i += 4 {
		if pixels[i] != 255 {
			x, y := (i/4)%postSize, (i/4)/postSize
			t.Fatalf("pixel (%d, %d) = %v, want red 255: the clear colour shows through an edge", x, y, pixels[i:i+3])
		}
	}
	if partlyCoveredPixels(r, 1) == 0 {
		t.Error("no pixel blends the white quad with the red background, want its edges smoothed")
	}
}

// TestMSAAFrameStepDrawsIntoTheScene: a step is told the scene is multisampled, and what
// it draws into SceneColor is part of the image the scene resolves to — the corners show
// the green it cleared the scene to after the opaque pass.
func TestMSAAFrameStepDrawsIntoTheScene(t *testing.T) {
	r, scene := stepScene(t)
	r.EnableAntiAliasing(true)
	var log []string
	green := [4]float32{0, 1, 0, 1}
	step := &recordingStep{name: "green", log: &log, clearColor: &green}
	r.AddFrameStep(pix.FrameStageAfterOpaque, step)

	r.Render(scene)

	if !step.frames[0].SceneDepth.IsValid() {
		t.Error("SceneDepth is zero, want the multisampled depth to test against")
	}
	if got, want := pixelAt(r, 0, 0), [3]byte{0, 255, 0}; got != want {
		t.Errorf("corner = %v, want %v, the step's green", got, want)
	}
}

// TestMSAABlendsOverTheResolvedScene: with MSAA and no frame step drawing after the
// opaque pass, the opaque pass resolves the scene as it ends and blended surfaces are
// drawn one sample a pixel over it. A 50% blue quad in front of an opaque red one must
// still blend with it, red and blue both showing — with the depth prepass too, whose
// multisampled depth the opaque pass loads rather than clears.
func TestMSAABlendsOverTheResolvedScene(t *testing.T) {
	for _, prepass := range []bool{false, true} {
		name := "no prepass"
		if prepass {
			name = "prepass"
		}
		t.Run(name, func(t *testing.T) {
			r, err := pix.NewOffscreenRenderer(postSize, postSize)
			if err != nil {
				t.Fatal(err)
			}
			defer r.Destroy()
			r.EnableAntiAliasing(true)
			r.EnableDepthPrepass(prepass)
			r.SetClearColor(colors.RGBA32F{0, 0, 0, 1})
			scene := scenes.New()
			defer scene.Destroy()

			quad := func(z float32) geometries.Geometry {
				return r.GeometryStore.Create(geometries.GeometryConfig{
					Attributes: []geometries.Attribute{
						geometries.NewAttribute(geometries.AttributePosition, geometries.Float32x3, []glm.Vec3f{{-0.8, -0.8, z}, {0.8, -0.8, z}, {0.8, 0.8, z}, {-0.8, 0.8, z}}),
					},
					Indices: []uint32{0, 1, 2, 0, 2, 3},
				})
			}
			red := r.NewBasicMaterial()
			red.SetColor(colors.RGBA32F{1, 0, 0, 1})
			blue := r.NewBasicMaterial()
			blue.SetColor(colors.RGBA32F{0, 0, 1, 0.5})
			blue.SetBlend(materials.BlendAlpha)
			scene.Add(scene.NewMesh(quad(-0.5), red))
			scene.Add(scene.NewMesh(quad(0), blue))
			cam := scene.NewPerspectiveCamera(45, 1, 0.1, 100)
			scene.Add(cam)
			cam.SetPosition(glm.Vec3f{0, 0, 2})
			r.Render(scene)

			// Half blue over red: (0.5, 0, 0.5) linear, 188 on the display.
			if got := pixelAt(r, postSize/2, postSize/2); absDiff(int(got[0]), 188) > 3 || got[1] != 0 || absDiff(int(got[2]), 188) > 3 {
				t.Errorf("centre = %v, want (188, 0, 188): blue blended half over red", got)
			}
		})
	}
}

// TestMSAAFrameStepAfterDepthTestsSamples: a step that runs between the depth prepass
// and the opaque pass is told the depth is multisampled, with MSAA, though the steps
// after the opaque pass — where there are none — would draw one sample a pixel.
func TestMSAAFrameStepAfterDepthTestsSamples(t *testing.T) {
	r, scene := stepScene(t)
	r.EnableAntiAliasing(true)
	var log []string
	step := &recordingStep{name: "after-depth", log: &log}
	r.AddFrameStep(pix.FrameStageAfterDepth, step)
	r.Render(scene)

	frame := step.frames[len(step.frames)-1]
	if frame.SceneSamples != 4 || !frame.SceneDepth.IsValid() {
		t.Errorf("after depth: SceneSamples = %d, SceneDepth valid = %v; want 4 and a depth to test against", frame.SceneSamples, frame.SceneDepth.IsValid())
	}
}
