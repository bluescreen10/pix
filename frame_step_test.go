package pix_test

import (
	"slices"
	"testing"

	"github.com/bluescreen10/gamekit/gpu"
	"github.com/bluescreen10/pix"
	"github.com/bluescreen10/pix/colors"
	"github.com/bluescreen10/pix/geometries"
	"github.com/bluescreen10/pix/glm"
	"github.com/bluescreen10/pix/materials"
	"github.com/bluescreen10/pix/scenes"
)

// recordingStep appends its name to a shared log each time it runs, keeps the frames it
// was given, and, when clearColor is set, clears the scene image to it.
type recordingStep struct {
	name       string
	log        *[]string
	frames     []pix.Frame
	clearColor *[4]float32
	releases   int
}

func (s *recordingStep) Encode(frame *pix.Frame, cmd gpu.CommandBuffer) {
	*s.log = append(*s.log, s.name)
	s.frames = append(s.frames, *frame)
	if s.clearColor == nil {
		return
	}
	cmd.BeginRenderPass(gpu.RenderTargets{
		Color: []gpu.ColorAttachment{{Texture: frame.SceneColor, Load: gpu.LoadClear, Store: gpu.StoreKeep, Clear: *s.clearColor}},
	})
	cmd.EndRenderPass()
}

func (s *recordingStep) Release() {
	s.releases++
}

// stepScene is an opaque red quad in the middle of the frame, over a black background,
// with a 50%-alpha blue quad in front of it. From 4 units away a 45-degree camera sees
// about ±1.66 of the plane z = 0, so the ±0.8 quads cover the centre and leave the
// corners empty.
func stepScene(t *testing.T) (*pix.Renderer, *scenes.Scene) {
	t.Helper()
	r, err := pix.NewOffscreenRenderer(postSize, postSize)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(r.Destroy)
	r.SetClearColor(colors.RGBA32F{0, 0, 0, 1})

	scene := scenes.New()
	t.Cleanup(scene.Destroy)
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
	scene.Add(scene.NewMesh(quad(0), red))
	blue := r.NewBasicMaterial()
	blue.SetColor(colors.RGBA32F{0, 0, 1, 0.5})
	blue.SetBlend(materials.BlendAlpha)
	scene.Add(scene.NewMesh(quad(0.5), blue))

	cam := scene.NewPerspectiveCamera(45, 1, 0.1, 100)
	scene.Add(cam)
	cam.SetPosition(glm.Vec3f{0, 0, 4})
	return r, scene
}

// TestFrameStepsRunInStageOrder: steps run stage by stage in frame order, and within a
// stage in the order they were added, whatever order the stages were added in.
func TestFrameStepsRunInStageOrder(t *testing.T) {
	r, scene := stepScene(t)
	var log []string
	add := func(stage pix.FrameStage, name string) {
		r.AddFrameStep(stage, &recordingStep{name: name, log: &log})
	}
	add(pix.FrameStageAfterTransparent, "after transparent")
	add(pix.FrameStageAfterOpaque, "after opaque")
	add(pix.FrameStageAfterDepth, "after depth")
	add(pix.FrameStageAfterShadows, "after shadows")
	add(pix.FrameStageStart, "frame start 1")
	add(pix.FrameStageStart, "frame start 2")

	r.Render(scene)

	want := []string{"frame start 1", "frame start 2", "after shadows", "after depth", "after opaque", "after transparent"}
	if !slices.Equal(log, want) {
		t.Errorf("steps ran as %q, want %q", log, want)
	}
}

// TestFrameStepsWithoutCameraRunOnlyEarlyStages: with no camera there is no main view,
// so the stages that follow its drawing do not happen, but the ones before it still do,
// so simulations keep advancing.
func TestFrameStepsWithoutCameraRunOnlyEarlyStages(t *testing.T) {
	r, _ := stepScene(t)
	scene := scenes.New()
	defer scene.Destroy()
	var log []string
	for _, stage := range []pix.FrameStage{pix.FrameStageStart, pix.FrameStageAfterShadows, pix.FrameStageAfterDepth, pix.FrameStageAfterOpaque, pix.FrameStageAfterTransparent} {
		r.AddFrameStep(stage, &recordingStep{name: stageName(stage), log: &log})
	}

	r.Render(scene)

	want := []string{"frame start", "after shadows"}
	if !slices.Equal(log, want) {
		t.Errorf("steps ran as %q, want %q", log, want)
	}
}

func stageName(stage pix.FrameStage) string {
	return [...]string{"frame start", "after shadows", "after depth", "after opaque", "after transparent"}[stage]
}

// TestFrameStepSeesOnlyWhatItsStageFilled: depth is handed over once it is filled, the
// scene image once it holds the opaque scene, and not before — a step handed an image
// early would read or draw over something the frame has not made yet.
func TestFrameStepSeesOnlyWhatItsStageFilled(t *testing.T) {
	r, scene := stepScene(t)
	var log []string
	steps := map[pix.FrameStage]*recordingStep{}
	for _, stage := range []pix.FrameStage{pix.FrameStageStart, pix.FrameStageAfterShadows, pix.FrameStageAfterDepth, pix.FrameStageAfterOpaque, pix.FrameStageAfterTransparent} {
		steps[stage] = &recordingStep{name: stageName(stage), log: &log}
		r.AddFrameStep(stage, steps[stage])
	}

	r.Render(scene)

	for stage, step := range steps {
		frame := step.frames[0]
		if got, want := frame.SceneDepth.IsValid(), stage >= pix.FrameStageAfterDepth; got != want {
			t.Errorf("%s: SceneDepth.IsValid() = %v, want %v", stageName(stage), got, want)
		}
		if got, want := frame.SceneColor.IsValid(), stage >= pix.FrameStageAfterOpaque; got != want {
			t.Errorf("%s: SceneColor.IsValid() = %v, want %v", stageName(stage), got, want)
		}
		if frame.ViewProj == (glm.Mat4f{}) {
			t.Errorf("%s: ViewProj is zero, want the main camera's", stageName(stage))
		}
		if frame.Eye != (glm.Vec3f{0, 0, 4}) {
			t.Errorf("%s: Eye = %v, want the camera at %v", stageName(stage), frame.Eye, glm.Vec3f{0, 0, 4})
		}
		if frame.Width != postSize || frame.Height != postSize {
			t.Errorf("%s: size = %dx%d, want %dx%d", stageName(stage), frame.Width, frame.Height, postSize, postSize)
		}
	}
}

// TestFrameStepClockAdvances: each render of a scene is the next frame number, and
// DeltaTime is the time between the two renders — 0 on the first, when there is no
// previous one.
func TestFrameStepClockAdvances(t *testing.T) {
	r, scene := stepScene(t)
	var log []string
	step := &recordingStep{name: "step", log: &log}
	r.AddFrameStep(pix.FrameStageStart, step)

	r.Render(scene)
	r.Render(scene)

	first, second := step.frames[0], step.frames[1]
	if second.Number != first.Number+1 {
		t.Errorf("frame numbers = %d, %d, want consecutive", first.Number, second.Number)
	}
	if first.DeltaTime != 0 {
		t.Errorf("first DeltaTime = %v, want 0", first.DeltaTime)
	}
	if want := second.Time - first.Time; second.DeltaTime != want {
		t.Errorf("second DeltaTime = %v, want %v, the time between the two renders", second.DeltaTime, want)
	}
}

// TestFrameStepAfterOpaqueDrawsUnderTransparent: a step after the opaque pass replaces
// the opaque scene, and blended surfaces are then drawn over what it left. The step
// clears to green: where the blue quad is, it blends over the green; where nothing is,
// the green stays; the red quad is gone everywhere.
func TestFrameStepAfterOpaqueDrawsUnderTransparent(t *testing.T) {
	r, scene := stepScene(t)
	var log []string
	green := [4]float32{0, 1, 0, 1}
	r.AddFrameStep(pix.FrameStageAfterOpaque, &recordingStep{name: "green", log: &log, clearColor: &green})

	r.Render(scene)

	if got, want := pixelAt(r, 0, 0), [3]byte{0, 255, 0}; got != want {
		t.Errorf("corner = %v, want %v, the step's green", got, want)
	}
	center := pixelAt(r, postSize/2, postSize/2)
	if center[0] != 0 || center[1] < 100 || center[2] < 100 {
		t.Errorf("center = %v, want no red, and the blue quad blended over the green", center)
	}
}

// TestFrameStepAfterTransparentSeesWholeScene: a step after the transparent pass comes
// after everything the scene draws, so what it draws is the final image: the centre,
// where both quads are, comes out the same as the empty corner. With HDR on the green
// is tone-mapped on the way, so only without it is the exact value known.
func TestFrameStepAfterTransparentSeesWholeScene(t *testing.T) {
	for _, hdr := range []bool{false, true} {
		t.Run(map[bool]string{false: "LDR", true: "HDR"}[hdr], func(t *testing.T) {
			r, scene := stepScene(t)
			r.EnableHDR(hdr)
			var log []string
			green := [4]float32{0, 1, 0, 1}
			r.AddFrameStep(pix.FrameStageAfterTransparent, &recordingStep{name: "green", log: &log, clearColor: &green})

			r.Render(scene)

			center, corner := pixelAt(r, postSize/2, postSize/2), pixelAt(r, 0, 0)
			if center != corner {
				t.Errorf("center = %v, corner = %v, want the step's green over both", center, corner)
			}
			if !hdr && center != [3]byte{0, 255, 0} {
				t.Errorf("center = %v, want %v", center, [3]byte{0, 255, 0})
			}
		})
	}
}

// TestFrameStepAfterDepthRunsPrepass: depth is filled ahead of shading only by the
// prepass, so a step that needs it turns the prepass on, though EnableDepthPrepass
// never did.
func TestFrameStepAfterDepthRunsPrepass(t *testing.T) {
	r, scene := stepScene(t)
	r.ShowFPS(true) // what turns timing on at all
	var log []string
	r.AddFrameStep(pix.FrameStageAfterDepth, &recordingStep{name: "step", log: &log})

	r.Render(scene)

	if !r.Profiler().IsPassRecorded(pix.GPUPassPrepass) {
		t.Error("the depth prepass did not run, but a step after it needs its depth")
	}
}

// TestRemovedFrameStepIsReleased: removing a step stops running it and releases it, and
// removing it again does nothing.
func TestRemovedFrameStepIsReleased(t *testing.T) {
	r, scene := stepScene(t)
	var log []string
	kept := &recordingStep{name: "kept", log: &log}
	removed := &recordingStep{name: "removed", log: &log}
	r.AddFrameStep(pix.FrameStageAfterOpaque, kept)
	r.AddFrameStep(pix.FrameStageAfterOpaque, removed)
	r.Render(scene)

	r.RemoveFrameStep(removed)
	r.RemoveFrameStep(removed)
	if removed.releases != 1 {
		t.Errorf("the removed step was released %d times, want once", removed.releases)
	}
	if kept.releases != 0 {
		t.Errorf("the kept step was released %d times, want none while it stays added", kept.releases)
	}
	r.Render(scene)
	if want := []string{"kept", "removed", "kept"}; !slices.Equal(log, want) {
		t.Errorf("steps ran as %q, want %q", log, want)
	}
}

// TestFrameStepsReleasedWithRenderer: destroying the renderer releases every step still
// added, since nothing will run them again.
func TestFrameStepsReleasedWithRenderer(t *testing.T) {
	r, err := pix.NewOffscreenRenderer(postSize, postSize)
	if err != nil {
		t.Fatal(err)
	}
	var log []string
	step := &recordingStep{name: "step", log: &log}
	r.AddFrameStep(pix.FrameStageStart, step)

	r.Destroy()

	if step.releases != 1 {
		t.Errorf("the step was released %d times, want once", step.releases)
	}
}

// TestFrameStepAddedTwicePanics: a step can be added once, at one stage.
func TestFrameStepAddedTwicePanics(t *testing.T) {
	r, _ := stepScene(t)
	var log []string
	step := &recordingStep{name: "step", log: &log}
	r.AddFrameStep(pix.FrameStageStart, step)

	defer func() {
		if recover() == nil {
			t.Error("AddFrameStep did not panic on a step already added")
		}
	}()
	r.AddFrameStep(pix.FrameStageAfterOpaque, step)
}
