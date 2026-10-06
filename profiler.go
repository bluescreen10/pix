// Frame profiling: where a frame's time went, on the CPU and on the GPU, overall and
// pass by pass. A total says the budget is gone; the breakdown says which pass spent it.
package pix

import (
	"time"

	"github.com/bluescreen10/gamekit/gpu"
)

// GPUPass names a phase of the frame that is timed separately, listed in the order the
// frame runs them. No pass is timed inside another, so their times add up to no more
// than the frame's.
type GPUPass uint8

const (
	// GPUPassCull is the compute cull for every view, plus skinning and particle
	// simulation — everything recorded before the first render pass.
	GPUPassCull GPUPass = iota
	// GPUPassShadow is the depth passes that fill the shadow maps, all lights and all
	// cascades together.
	GPUPassShadow
	// GPUPassLightClusters is the compute pass that lists each light cluster's point and
	// spot lights.
	GPUPassLightClusters
	// GPUPassPrepass is the depth-only pass that precedes forward shading, and does not
	// run unless Renderer.EnableDepthPrepass turned it on.
	GPUPassPrepass
	// GPUPassOpaque is the opaque pass: every opaque surface shaded.
	GPUPassOpaque
	// GPUPassAmbientOcclusion measures ambient occlusion from the opaque scene's depth,
	// and darkens the scene by it when that needs a pass of its own. It does not run
	// unless ambient occlusion is enabled or shown.
	GPUPassAmbientOcclusion
	// GPUPassTransparent is everything drawn over the opaque scene: the backgrounds, the
	// blended surfaces and particles, the frame steps between them, and resolving the
	// multisampled scene.
	GPUPassTransparent
	// GPUPassPostProcessing is an HDR frame's post-processing chain and the tone-map
	// pass that follows it. Without HDR it does not run.
	GPUPassPostProcessing

	gpuPassCount
)

// gpuPassNames is each pass's label, in enum order.
var gpuPassNames = [gpuPassCount]string{"cull", "shadow", "clusters", "prepass", "opaque", "occlusion", "transparent", "post"}

// String returns the pass's label.
func (p GPUPass) String() string {
	if int(p) < len(gpuPassNames) {
		return gpuPassNames[p]
	}
	return "?"
}

// GPUPassNames lists every pass label, in enum order.
func GPUPassNames() []string {
	return gpuPassNames[:]
}

// Wait names a stretch of a frame the CPU spends blocked rather than working. Each is
// timed on its own, and none of them counts toward CPUTime.
type Wait uint8

const (
	// WaitAcquire is blocking until the swapchain hands over an image to draw into —
	// which is mostly waiting for vsync. Zero when rendering offscreen.
	WaitAcquire Wait = iota
	// WaitSubmit is submitting the frame and blocking until it is done: presentation
	// for a window, the GPU finishing for an offscreen target.
	WaitSubmit

	waitCount
)

// waitNames is each wait's label, in enum order.
var waitNames = [waitCount]string{"acquire", "submit"}

// String returns the wait's label.
func (w Wait) String() string {
	if int(w) < len(waitNames) {
		return waitNames[w]
	}
	return "?"
}

const (
	// profileSamples is how many frames the CPU and wall-clock averages span.
	profileSamples = 60

	// profileWarmup is how many frames of GPU timings are discarded before anything
	// is reported. The first frames carry one-off pipeline and shader compilation, and
	// an average seeded from one of those takes seconds to come back down.
	profileWarmup = 3

	// profileSmoothing is how far each GPU sample moves its average. A figure that
	// jitters by a factor of two between frames cannot be read off a HUD, and the
	// question these answer — which pass is worth optimizing — is about the steady
	// state rather than any one frame.
	profileSmoothing = 0.1
)

// The timestamp pool holds a start and an end timestamp for each pass, then the frame's
// own start and end. One pool for all of it, so the total and the passes are read back
// together and cannot disagree about which frame they describe.
const (
	frameStartSlot   = uint32(gpuPassCount) * 2
	frameEndSlot     = frameStartSlot + 1
	profileSlotCount = frameEndSlot + 1
)

// passStartSlot and passEndSlot are where a pass's two timestamps live in the pool.
func passStartSlot(pass GPUPass) uint32 {
	return uint32(pass) * 2
}

func passEndSlot(pass GPUPass) uint32 {
	return uint32(pass)*2 + 1
}

// Profiler reports where recent frames spent their time.
//
// CPU and frame times are rolling averages over the last profileSamples frames. GPU
// times come from timestamps written into the frame's own command buffer and read back
// once it has drained, smoothed the same way for the total and for every pass. GPU
// profiling runs only while stats are visible (see Renderer.ShowFPS), because a
// timestamp per pass is not free and a shipped frame should not pay for a HUD nobody is
// looking at; the GPU figures read zero until then.
//
// The zero value is ready to use.
type Profiler struct {
	// Wall-clock, CPU and wait times, as rings over the last profileSamples frames.
	frameTimes [profileSamples]time.Duration
	cpuTimes   [profileSamples]time.Duration
	waitTimes  [waitCount][profileSamples]time.Duration
	frameCount int
	frameStart time.Time
	// waitStart is when each wait in progress began; zero when it is not in progress.
	waitStart [waitCount]time.Time

	// GPU timestamps. passStarted and passEnded record which passes this frame actually
	// bracketed, which is how a pass that ran is told apart from one that did not — the
	// timestamps themselves cannot say (see IsPassRecorded).
	timestampPool gpu.QueryPool
	passStarted   [gpuPassCount]bool
	passEnded     [gpuPassCount]bool

	// Smoothed GPU times, and how many frames' timestamps have been read back.
	gpuFrameMS    float64
	gpuPassMS     [gpuPassCount]float64
	gpuFramesRead int
}

// FPS is frames per second, from the average wall-clock frame time.
func (p *Profiler) FPS() float64 {
	frame := p.FrameTime().Seconds()
	if frame <= 0 {
		return 0
	}
	return 1 / frame
}

// FrameTime is the average wall-clock time from the start of one Render to its end.
func (p *Profiler) FrameTime() time.Duration {
	return p.average(&p.frameTimes)
}

// CPUTime is the average CPU cost of producing a frame: its wall-clock time minus every
// Wait, which leaves the time the renderer was actually working — scene preparation,
// command encoding, and the bookkeeping after submission.
func (p *Profiler) CPUTime() time.Duration {
	return p.average(&p.cpuTimes)
}

// WaitTime is the average time a frame spent blocked in one Wait.
func (p *Profiler) WaitTime(wait Wait) time.Duration {
	if wait >= waitCount {
		return 0
	}
	return p.average(&p.waitTimes[wait])
}

// GPUTime is the smoothed time the GPU spent executing a frame's commands.
func (p *Profiler) GPUTime() time.Duration {
	return durationFromMS(p.gpuFrameMS)
}

// PassTime is the smoothed GPU time one pass of the frame took. Zero when the pass did
// not run — and also when it ran but could not be measured; IsPassRecorded tells the
// two apart.
func (p *Profiler) PassTime(pass GPUPass) time.Duration {
	if pass >= gpuPassCount {
		return 0
	}
	return durationFromMS(p.gpuPassMS[pass])
}

// IsPassRecorded reports whether the last frame actually performed a pass, which is not
// the same as it having a time: a pass can run and still measure zero.
//
// Whether a timestamp resolves where it was written is the backend's business, and not
// every backend can resolve one everywhere. KosmicKrisp samples only at render-pass
// boundaries, so a write made outside one is deferred to the next pass's start — which
// collapses the two ends of a compute-only pass onto the same tick and makes the cull
// measure exactly zero there. It ran; it cannot be timed. Reporting that as "did not
// run" would be a lie about the frame.
func (p *Profiler) IsPassRecorded(pass GPUPass) bool {
	if pass >= gpuPassCount {
		return false
	}
	return p.passStarted[pass] && p.passEnded[pass]
}

// --- CPU side, driven by Render ---

// beginFrame starts the frame's clock and clears its waits.
func (p *Profiler) beginFrame() {
	p.frameCount++
	p.frameStart = time.Now()
	for wait := range waitCount {
		p.waitTimes[wait][p.ringIndex()] = 0
		p.waitStart[wait] = time.Time{}
	}
}

// beginWait and endWait bracket a stretch where the renderer is blocked rather than
// working. The waiting is part of the frame but not part of what the frame cost the
// CPU, and the frame's working stretches sit either side of the acquire — so the
// profiler is told where the waiting is rather than guessing it from one start and one
// end. A wait may be bracketed more than once in a frame; the stretches add up.
func (p *Profiler) beginWait(wait Wait) {
	p.waitStart[wait] = time.Now()
}

func (p *Profiler) endWait(wait Wait) {
	if p.waitStart[wait].IsZero() {
		return
	}
	p.waitTimes[wait][p.ringIndex()] += time.Since(p.waitStart[wait])
	p.waitStart[wait] = time.Time{}
}

// endFrame closes the frame's clock and works out its CPU time: whatever part of the
// frame was not spent in a wait.
func (p *Profiler) endFrame() {
	frameTime := time.Since(p.frameStart)
	cpuTime := frameTime
	for wait := range waitCount {
		cpuTime -= p.waitTimes[wait][p.ringIndex()]
	}
	p.frameTimes[p.ringIndex()] = frameTime
	p.cpuTimes[p.ringIndex()] = cpuTime
}

// ringIndex is this frame's position in the CPU and frame-time rings.
func (p *Profiler) ringIndex() int {
	return (p.frameCount - 1) % profileSamples
}

// average is the mean of the filled part of a ring, so the first frames are not
// diluted by the still-zero tail.
func (p *Profiler) average(ring *[profileSamples]time.Duration) time.Duration {
	n := min(p.frameCount, profileSamples)
	if n == 0 {
		return 0
	}
	var total time.Duration
	for _, d := range ring[:n] {
		total += d
	}
	return total / time.Duration(n)
}

// --- GPU side, recorded into the frame by encode ---

// isGPUProfilingEnabled reports whether GPU timestamps are being recorded, which is
// true between enableGPUProfiling and disableGPUProfiling.
func (p *Profiler) isGPUProfilingEnabled() bool {
	return p.timestampPool.IsValid()
}

// enableGPUProfiling creates the timestamp pool, which is what turns GPU profiling on.
func (p *Profiler) enableGPUProfiling(backend gpu.Backend) {
	if !p.timestampPool.IsValid() {
		p.timestampPool = backend.CreateTimestampPool(profileSlotCount)
	}
}

// disableGPUProfiling drops the timestamp pool, which turns GPU profiling off. The
// figures already gathered are kept, but a pass that stops being measured is not one
// that stopped running, so no pass is reported as recorded any more.
func (p *Profiler) disableGPUProfiling(backend gpu.Backend) {
	if p.timestampPool.IsValid() {
		backend.DestroyTimestampPool(p.timestampPool)
	}
	p.timestampPool = gpu.QueryPool{}
	p.passStarted, p.passEnded = [gpuPassCount]bool{}, [gpuPassCount]bool{}
}

// beginGPUFrame resets the pool and stamps the frame's start. Every slot is cleared, so
// a pass that does not run this frame reads back as unwritten rather than as last
// frame's value.
//
// The start uses StageNone, which is not "no particular stage": it means the command
// buffer's own start, resolved from the submission's GPU boundaries. That is exactly
// what the frame total wants, and exactly what a pass must never use — see beginPass.
func (p *Profiler) beginGPUFrame(cmd gpu.CommandBuffer) {
	if !p.isGPUProfilingEnabled() {
		return
	}
	cmd.ResetTimestamps(p.timestampPool, profileSlotCount)
	cmd.WriteTimestamp(p.timestampPool, frameStartSlot, gpu.StageNone)
	p.passStarted, p.passEnded = [gpuPassCount]bool{}, [gpuPassCount]bool{}
}

// beginPass and endPass bracket one pass. endPass is tolerant of a pass that was never
// begun, so a caller can bracket a block that sometimes does nothing without guarding
// both ends.
//
// Neither end may use StageNone: that resolves to the command buffer's start, so every
// pass would appear to begin when the frame did and the later passes would each measure
// almost the whole frame. Any real stage samples where the write actually sits.
func (p *Profiler) beginPass(pass GPUPass, cmd gpu.CommandBuffer) {
	if !p.isGPUProfilingEnabled() {
		return
	}
	p.passStarted[pass] = true
	cmd.WriteTimestamp(p.timestampPool, passStartSlot(pass), gpu.StageCompute)
}

func (p *Profiler) endPass(pass GPUPass, cmd gpu.CommandBuffer) {
	if !p.isGPUProfilingEnabled() || !p.passStarted[pass] {
		return
	}
	p.passEnded[pass] = true
	cmd.WriteTimestamp(p.timestampPool, passEndSlot(pass), gpu.StageAll)
}

// writeSkippedPasses writes both timestamps for every pass that did not run. Required
// rather than tidy: a backend may read the pool by waiting for all of its results, and
// a query that was reset but never written never becomes available — leaving a skipped
// pass unwritten hangs that read forever.
//
// It must be recorded while the frame's last render pass is still open; see encode for
// why that placement is load-bearing.
func (p *Profiler) writeSkippedPasses(cmd gpu.CommandBuffer) {
	if !p.isGPUProfilingEnabled() {
		return
	}
	for pass := range gpuPassCount {
		if !p.passStarted[pass] {
			cmd.WriteTimestamp(p.timestampPool, passStartSlot(pass), gpu.StageCompute)
		}
		if !p.passEnded[pass] {
			cmd.WriteTimestamp(p.timestampPool, passEndSlot(pass), gpu.StageAll)
		}
	}
}

// endGPUFrame stamps the frame's end. It must be recorded after the frame's last render
// pass has ended; see encode.
func (p *Profiler) endGPUFrame(cmd gpu.CommandBuffer) {
	if !p.isGPUProfilingEnabled() {
		return
	}
	cmd.WriteTimestamp(p.timestampPool, frameEndSlot, gpu.StageAll)
}

// readGPUTimestamps pulls the frame's timestamps back and folds them into the averages.
// Called once the frame has been submitted and drained.
func (p *Profiler) readGPUTimestamps(backend gpu.Backend) {
	if !p.isGPUProfilingEnabled() {
		return
	}
	timestamps := backend.ReadTimestamps(p.timestampPool, profileSlotCount)
	if timestamps == nil {
		return
	}
	p.gpuFramesRead++
	if p.gpuFramesRead <= profileWarmup {
		return
	}

	msPerTick := backend.TimestampPeriod() / 1e6
	elapsedMS := func(startSlot, endSlot uint32) (float64, bool) {
		start, end := timestamps[startSlot], timestamps[endSlot]
		// A zero is a query that was never written: a backend may drop a write it cannot
		// attach to any work, and subtracting it from a real timestamp gives the age of
		// the GPU clock. That is a MISSING sample, not a measurement of zero.
		if start == 0 || end <= start {
			return 0, false
		}
		return float64(end-start) * msPerTick, true
	}
	firstSample := p.gpuFramesRead == profileWarmup+1

	if ms, ok := elapsedMS(frameStartSlot, frameEndSlot); ok {
		p.gpuFrameMS = smooth(p.gpuFrameMS, ms, firstSample)
	}

	for pass := range gpuPassCount {
		// Whether a pass ran is known from the recording, not inferred from its
		// timestamps: writeSkippedPasses's two writes for a pass that did NOT run still
		// land a few nanoseconds apart, and reporting that as a duration lists passes
		// the frame never performed.
		if !p.IsPassRecorded(pass) {
			p.gpuPassMS[pass] = 0
			continue
		}
		// A missing sample keeps the last good value rather than averaging toward zero.
		if ms, ok := elapsedMS(passStartSlot(pass), passEndSlot(pass)); ok {
			// A pass that was not running last frame has no average to continue.
			p.gpuPassMS[pass] = smooth(p.gpuPassMS[pass], ms, firstSample || p.gpuPassMS[pass] == 0)
		}
	}
}

// smooth folds one sample into a running average — or, for the first sample, starts
// the average from it.
func smooth(average, sample float64, firstSample bool) float64 {
	if firstSample {
		return sample
	}
	return average + (sample-average)*profileSmoothing
}

// durationFromMS converts a millisecond count to a Duration.
func durationFromMS(ms float64) time.Duration {
	return time.Duration(ms * float64(time.Millisecond))
}
