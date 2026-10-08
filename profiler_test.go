package pix_test

import (
	"testing"
	"time"

	"github.com/bluescreen10/pix"
	"github.com/bluescreen10/pix/colors"
	"github.com/bluescreen10/pix/glm"
	"github.com/bluescreen10/pix/scenes"
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
	r, err := pix.NewOffscreenRenderer(128, 128)
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
	box := scene.NewMesh(r.GeometryStore.Create(pix.BoxGeometry(40, 40, 40)), r.NewPBRMaterial())
	box.SetPosition(glm.Vec3f{0, 0, -120})

	cam := scene.NewPerspectiveCamera(60, 1, 1, 5000)
	cam.SetPosition(glm.Vec3f{0, 0, 0})
	cam.LookAt(glm.Vec3f{0, 0, -1})
	for range 4 {
		r.Render(scene)
	}

	// Rendering forward with shadows on: these happen every frame.
	for _, p := range []pix.GPUPass{pix.GPUPassCull, pix.GPUPassShadow, pix.GPUPassOpaque} {
		if !r.Profiler().IsPassRecorded(p) {
			t.Errorf("%s did not report as recorded, but a forward frame with shadows runs it", p)
		}
	}
	// These do not — the prepass is off, and without HDR there is no post pass — and
	// must not merely read as zero.
	for _, p := range []pix.GPUPass{pix.GPUPassPrepass, pix.GPUPassPostProcessing} {
		if r.Profiler().IsPassRecorded(p) {
			t.Errorf("%s reported as recorded, but this frame does not run it", p)
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
	r, err := pix.NewOffscreenRenderer(128, 128)
	if err != nil {
		t.Fatal(err)
	}
	defer r.Destroy()

	scene := scenes.New()
	defer scene.Destroy()
	scene.SetAmbient(colors.RGB32F{0.3, 0.3, 0.3}, 1)
	scene.NewMesh(r.GeometryStore.Create(pix.BoxGeometry(40, 40, 40)), r.NewPBRMaterial())
	cam := scene.NewPerspectiveCamera(60, 1, 1, 5000)
	cam.SetPosition(glm.Vec3f{0, 0, 120})
	for range 8 {
		r.Render(scene)
	}

	p := r.Profiler()
	if got := p.WaitTime(pix.WaitAcquire); got != 0 {
		t.Errorf("WaitTime(WaitAcquire) = %v offscreen, want 0: there is no swapchain to acquire from", got)
	}
	if got := p.WaitTime(pix.WaitSubmit); got <= 0 {
		t.Errorf("WaitTime(WaitSubmit) = %v, want > 0: submitting blocks until the GPU finishes", got)
	}
	if got := p.CPUTime(); got <= 0 {
		t.Errorf("CPUTime() = %v, want > 0", got)
	}
	// The pieces account for the whole frame and no more. Each average rounds its own
	// integer division down, so the three can fall short of the frame's by a nanosecond
	// apiece; anything beyond that is time counted twice or not at all.
	sum := p.CPUTime() + p.WaitTime(pix.WaitAcquire) + p.WaitTime(pix.WaitSubmit)
	if diff := p.FrameTime() - sum; diff < 0 || diff > 3*time.Nanosecond {
		t.Errorf("CPUTime + waits = %v, want FrameTime() = %v", sum, p.FrameTime())
	}
}

// TestPassTimesAddUpToNoMoreThanTheFrame: the passes are parts of the frame, so their
// times together cannot exceed the frame's — however much a GPU overlaps one pass with
// the next, and with every pass running, ambient occlusion included.
func TestPassTimesAddUpToNoMoreThanTheFrame(t *testing.T) {
	r, err := pix.NewOffscreenRenderer(256, 256)
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
	for i := range pix.GPUPassNames() {
		passes += r.Profiler().PassTime(pix.GPUPass(i))
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
