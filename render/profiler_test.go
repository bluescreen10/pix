package render_test

import (
	"fmt"
	"testing"
	"time"

	"github.com/bluescreen10/gamekit/gpu"

	"github.com/bluescreen10/pix/colors"
	"github.com/bluescreen10/pix/glm"
	"github.com/bluescreen10/pix/render"
	"github.com/bluescreen10/pix/render/scenes"
)

// TestPassRecordedSeparatesUnrunFromUnmeasured: a pass that ran and a pass that did
// not are different facts about the frame, and a time of zero cannot tell them apart.
//
// It matters because it is not hypothetical — KosmicKrisp resolves timestamps only at
// render-pass boundaries, so the compute-only cull pass measures exactly zero there
// while running perfectly well. Keying the HUD off the time alone hid it and said the
// frame never culled.
//
// Deliberately asserts nothing about durations: what a backend can measure is its own
// business, and the values differ between Metal and Vulkan for exactly that reason.
func TestPassRecordedSeparatesUnrunFromUnmeasured(t *testing.T) {
	r, err := render.NewOffscreenRenderer(128, 128)
	if err != nil {
		t.Fatal(err)
	}
	defer r.Destroy()
	r.ShowFPS(true) // what turns timing on at all
	r.EnableShadows(true)

	scene := scenes.New()
	defer scene.Destroy()
	scene.SetAmbient(colors.RGB32F{0.3, 0.3, 0.3}, 1)
	light := scene.AddDirectionalLight(glm.Vec3f{-0.4, -1, -0.3}, colors.RGB32F{1, 1, 1}, 2)
	light.SetCastShadow(true)
	box := scene.NewMesh(r.GeometryStore.Create(render.BoxGeometry(40, 40, 40)), r.NewPBRMaterial())
	box.SetPosition(glm.Vec3f{0, 0, -120})

	cam := scene.NewPerspectiveCamera(60, 1, 1, 5000)
	cam.SetPosition(glm.Vec3f{0, 0, 0})
	cam.LookAt(glm.Vec3f{0, 0, -1})
	for range 4 {
		r.Render(scene)
	}

	// Rendering forward with shadows on: these happen every frame.
	for _, name := range []string{"cull", "shadow", "opaque"} {
		if got, _ := gpuTiming(r.Profiler(), name); !got.IsRecorded {
			t.Errorf("%s did not report as recorded, but a forward frame with shadows runs it", name)
		}
	}
	// These do not — the prepass is off, and without HDR there is no post pass — and
	// must not merely read as zero.
	for _, name := range []string{"prepass", "post"} {
		if got, _ := gpuTiming(r.Profiler(), name); got.IsRecorded {
			t.Errorf("%s reported as recorded, but this frame does not run it", name)
		}
	}
}

// TestWaitsAreSeparatedFromCPUTime: a frame's time splits into the renderer working and
// the renderer blocked, and the profiler must keep the two apart — CPU time that
// included the wait for the GPU would say the CPU is the bottleneck whenever the GPU is.
//
// Offscreen there is no swapchain, so there is nothing to acquire; submitting still
// blocks until the GPU finishes the frame.
func TestWaitsAreSeparatedFromCPUTime(t *testing.T) {
	r, err := render.NewOffscreenRenderer(128, 128)
	if err != nil {
		t.Fatal(err)
	}
	defer r.Destroy()

	scene := scenes.New()
	defer scene.Destroy()
	scene.SetAmbient(colors.RGB32F{0.3, 0.3, 0.3}, 1)
	scene.NewMesh(r.GeometryStore.Create(render.BoxGeometry(40, 40, 40)), r.NewPBRMaterial())
	cam := scene.NewPerspectiveCamera(60, 1, 1, 5000)
	cam.SetPosition(glm.Vec3f{0, 0, 120})
	for range 8 {
		r.Render(scene)
	}

	p := r.Profiler()
	if got := p.WaitTime(render.WaitAcquire); got != 0 {
		t.Errorf("WaitTime(WaitAcquire) = %v offscreen, want 0: there is no swapchain to acquire from", got)
	}
	if got := p.WaitTime(render.WaitSubmit); got <= 0 {
		t.Errorf("WaitTime(WaitSubmit) = %v, want > 0: submitting blocks until the GPU finishes", got)
	}
	if got := p.CPUTime(); got <= 0 {
		t.Errorf("CPUTime() = %v, want > 0", got)
	}
	// The pieces account for the whole frame and no more. Each average rounds its own
	// integer division down, so the three can fall short of the frame's by a nanosecond
	// apiece; anything beyond that is time counted twice or not at all.
	sum := p.CPUTime() + p.WaitTime(render.WaitAcquire) + p.WaitTime(render.WaitSubmit)
	if diff := p.FrameTime() - sum; diff < 0 || diff > 3*time.Nanosecond {
		t.Errorf("CPUTime + waits = %v, want FrameTime() = %v", sum, p.FrameTime())
	}
}

// TestPassTimesAddUpToNoMoreThanTheFrame: the passes are parts of the frame, so their
// times together cannot exceed the frame's — however much a GPU overlaps one pass with
// the next, and with every pass running, ambient occlusion included.
func TestPassTimesAddUpToNoMoreThanTheFrame(t *testing.T) {
	r, err := render.NewOffscreenRenderer(256, 256)
	if err != nil {
		t.Fatal(err)
	}
	defer r.Destroy()
	r.ShowFPS(true)
	r.EnableHDR(true)
	r.EnableShadows(true)
	r.EnableAntiAliasing(true)
	r.EnableAmbientOcclusion(true)

	scene := scenes.New()
	defer scene.Destroy()
	scene.SetAmbient(colors.RGB32F{0.3, 0.3, 0.3}, 1)
	light := scene.AddDirectionalLight(glm.Vec3f{-0.4, -1, -0.3}, colors.RGB32F{1, 1, 1}, 2)
	light.SetCastShadow(true)
	scene.NewMesh(r.NewPlaneGeometry(40, 40, 1, 1), r.NewPBRMaterial())
	box := scene.NewMesh(r.NewBoxGeometry(2, 2, 2), r.NewPBRMaterial())
	box.SetPosition(glm.Vec3f{0, 1, -4})
	cam := scene.NewPerspectiveCamera(60, 1, 0.1, 100)
	cam.SetPosition(glm.Vec3f{0, 2, 3})
	cam.LookAt(glm.Vec3f{0, 0.5, -4})
	for range 30 {
		r.Render(scene)
	}

	var passes time.Duration
	for _, name := range []string{"cull", "shadow", "clusters", "prepass", "opaque", "occlusion", "transparent", "post"} {
		got, _ := gpuTiming(r.Profiler(), name)
		passes += got.Time
	}
	frame := r.Profiler().GPUTime()
	t.Logf("passes %v, frame %v", passes, frame)
	if frame == 0 {
		t.Skip("this backend measures no GPU time")
	}
	if passes > frame+frame/100 {
		t.Errorf("passes add up to %v, more than the frame's %v", passes, frame)
	}
}

// TestFrameStepIsTimedUnderItsTypeName: the renderer times each frame step by itself,
// so a sky or a simulation shows up on its own rather than hidden in a pass or in no
// pass at all — taking time, but no more than the frame it is part of.
func TestFrameStepIsTimedUnderItsTypeName(t *testing.T) {
	r, scene := stepScene(t)
	r.ShowFPS(true)
	var log []string
	clear := &recordingStep{name: "clear", log: &log, clearColor: &[4]float32{0, 0, 0, 1}}
	r.AddFrameStep(render.FrameStageAfterOpaque, clear)
	for range 10 {
		r.Render(scene)
	}

	got, ok := gpuTiming(r.Profiler(), "render_test.recordingStep")
	if !ok {
		t.Fatalf("GPUTimings() does not list render_test.recordingStep: %+v", r.Profiler().GPUTimings())
	}
	if !got.IsRecorded {
		t.Errorf("render_test.recordingStep = %+v, want recorded", got)
	}
	frame := r.Profiler().GPUTime()
	if frame == 0 {
		t.Skip("this backend measures no GPU time")
	}
	if got.Time <= 0 || got.Time > frame {
		t.Errorf("render_test.recordingStep = %+v, want more than 0 and at most the frame's %v", got, frame)
	}
}

// TestCPUTimerMeasuresWhatItBrackets: a CPU timer is listed under its name with the
// time spent between its start and its stop.
func TestCPUTimerMeasuresWhatItBrackets(t *testing.T) {
	r, err := render.NewOffscreenRenderer(64, 64)
	if err != nil {
		t.Fatal(err)
	}
	defer r.Destroy()
	scene := scenes.New()
	defer scene.Destroy()

	p := r.Profiler()
	timer := p.StartCPUTimer("work")
	time.Sleep(2 * time.Millisecond)
	p.StopCPUTimer(timer)
	r.Render(scene)

	timings := p.CPUTimings()
	if len(timings) != 1 {
		t.Fatalf("CPUTimings() lists %d timers, want 1", len(timings))
	}
	got := timings[0]
	if got.Name != "work" || !got.IsRecorded || got.Time < 2*time.Millisecond {
		t.Errorf("CPUTimings()[0] = %+v, want work, recorded, at least 2ms", got)
	}
}

// TestCPUTimerAddsUpItsStretches: starting a name again is the same timer, so a frame
// that times the same work twice lists it once, with both stretches.
func TestCPUTimerAddsUpItsStretches(t *testing.T) {
	r, err := render.NewOffscreenRenderer(64, 64)
	if err != nil {
		t.Fatal(err)
	}
	defer r.Destroy()
	scene := scenes.New()
	defer scene.Destroy()

	p := r.Profiler()
	for range 2 {
		timer := p.StartCPUTimer("work")
		time.Sleep(2 * time.Millisecond)
		p.StopCPUTimer(timer)
	}
	r.Render(scene)

	timings := p.CPUTimings()
	if len(timings) != 1 {
		t.Fatalf("CPUTimings() lists %d timers, want 1", len(timings))
	}
	if got := timings[0].Time; got < 4*time.Millisecond {
		t.Errorf("CPUTimings()[0].Time = %v, want at least 4ms", got)
	}
}

// gpuTimedStep times the step it wraps with a GPU timer of its own.
type gpuTimedStep struct {
	*recordingStep
	timerName string
}

func (s *gpuTimedStep) Encode(frame *render.Frame, cmd gpu.CommandBuffer) {
	timer := frame.Profiler.StartGPUTimer(s.timerName, cmd)
	s.recordingStep.Encode(frame, cmd)
	frame.Profiler.StopGPUTimer(timer, cmd)
}

// TestGPUTimerMeasuresWhatItBrackets: a frame step can time its own work on the GPU,
// listed under the timer's name, taking time but no more than the frame it is part of.
func TestGPUTimerMeasuresWhatItBrackets(t *testing.T) {
	r, scene := stepScene(t)
	r.ShowFPS(true)
	var log []string
	clear := &recordingStep{name: "clear", log: &log, clearColor: &[4]float32{0, 0, 0, 1}}
	r.AddFrameStep(render.FrameStageAfterOpaque, &gpuTimedStep{recordingStep: clear, timerName: "clear"})
	for range 10 {
		r.Render(scene)
	}

	frame := r.Profiler().GPUTime()
	if frame == 0 {
		t.Skip("this backend measures no GPU time")
	}
	got, ok := gpuTiming(r.Profiler(), "clear")
	if !ok {
		t.Fatalf("GPUTimings() does not list clear: %+v", r.Profiler().GPUTimings())
	}
	if !got.IsRecorded || got.Time <= 0 || got.Time > frame {
		t.Errorf("clear = %+v, want recorded, more than 0 and at most the frame's %v", got, frame)
	}
}

// gpuTiming finds the GPU timing named name.
func gpuTiming(p *render.Profiler, name string) (render.Timing, bool) {
	for _, timing := range p.GPUTimings() {
		if timing.Name == name {
			return timing, true
		}
	}
	return render.Timing{}, false
}

// manyGPUTimersStep times each of count clears with a GPU timer of its own.
type manyGPUTimersStep struct {
	*recordingStep
	count int
}

func (s *manyGPUTimersStep) Encode(frame *render.Frame, cmd gpu.CommandBuffer) {
	for i := range s.count {
		timer := frame.Profiler.StartGPUTimer(fmt.Sprintf("clear %d", i), cmd)
		s.recordingStep.Encode(frame, cmd)
		frame.Profiler.StopGPUTimer(timer, cmd)
	}
}

// TestGPUTimersAreNotLimitedInNumber: a frame may start more GPU timers than the
// profiler first has room for, and all of them are timed.
func TestGPUTimersAreNotLimitedInNumber(t *testing.T) {
	r, scene := stepScene(t)
	r.ShowFPS(true)
	var log []string
	clear := &recordingStep{name: "clear", log: &log, clearColor: &[4]float32{0, 0, 0, 1}}
	const count = 100
	r.AddFrameStep(render.FrameStageAfterOpaque, &manyGPUTimersStep{recordingStep: clear, count: count})
	for range 10 {
		r.Render(scene)
	}

	if r.Profiler().GPUTime() == 0 {
		t.Skip("this backend measures no GPU time")
	}
	for i := range count {
		name := fmt.Sprintf("clear %d", i)
		if got, ok := gpuTiming(r.Profiler(), name); !ok || !got.IsRecorded || got.Time <= 0 {
			t.Errorf("%s = %+v (listed %v), want recorded and more than 0", name, got, ok)
		}
	}
}
