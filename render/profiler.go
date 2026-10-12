// Frame profiling: where a frame's time went, on the CPU and on the GPU, overall and
// timer by timer. A total says the budget is gone; the breakdown says what spent it.
package render

import (
	"time"

	"github.com/bluescreen10/gamekit/gpu"
)

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
	// question these answer — which part is worth optimizing — is about the steady
	// state rather than any one frame.
	profileSmoothing = 0.1
)

// initialGPUTimerSpans is how many GPU timer spans the timestamp pool first holds. A
// frame that starts more grows it for the frames after.
const initialGPUTimerSpans = 32

// The timestamp pool holds the frame's start and end, then a start and an end for each
// GPU timer span, in the order the frame starts them. One pool for all of it, so the
// total and the timers are read back together and cannot disagree about which frame
// they describe.
const (
	frameStartSlot    = 0
	frameEndSlot      = 1
	firstGPUTimerSlot = 2
)

// gpuTimerStartSlot and gpuTimerEndSlot are where the timestamps of the frame's i-th
// GPU timer span live in the pool.
func gpuTimerStartSlot(i int) uint32 {
	return firstGPUTimerSlot + uint32(i)*2
}

func gpuTimerEndSlot(i int) uint32 {
	return firstGPUTimerSlot + uint32(i)*2 + 1
}

// Timing is how long a timer's work took in recent frames: a CPU timer's averaged as
// CPUTime is, a GPU timer's smoothed as GPUTime is. A timer started and stopped more
// than once in a frame counts all of it.
//
// IsRecorded reports whether the last frame ran the timer at all, which Time cannot
// say: a GPU timer can run and still measure zero (see GPUTimings).
type Timing struct {
	Name       string
	Time       time.Duration
	IsRecorded bool
}

// CPUTimer is a handle on a named CPU timer; see Profiler.StartCPUTimer.
type CPUTimer struct {
	index int
}

// cpuTimer is a CPU timer's state: its stretch in progress, the time it has measured
// since the last frame ended, and a ring of the frames before.
type cpuTimer struct {
	name        string
	start       time.Time // zero while stopped
	pendingTime time.Duration

	times      [profileSamples]time.Duration
	frameCount int // frames ended since the timer was first started
	isRecorded bool
}

// GPUTimer is a handle on a named GPU timer; see Profiler.StartGPUTimer.
type GPUTimer struct {
	index int
}

// gpuTimer is a GPU timer's smoothed time, and what the frame has done with it.
type gpuTimer struct {
	name       string
	averageMS  float64
	isRecorded bool // whether the last frame read back ran it

	// openSpan is the frame's span of the timer in progress, when hasOpenSpan.
	openSpan    int
	hasOpenSpan bool

	// What the frame being read back measured: how many of its spans could be measured,
	// and their time together.
	frameMeasuredSpans int
	frameMS            float64
}

// Profiler reports where recent frames spent their time.
//
// CPU and frame times are rolling averages over the last profileSamples frames. GPU
// times come from timestamps written into the frame's own command buffer and read back
// once it has drained, smoothed the same way for the total and for every timer. GPU
// profiling runs only while stats are visible (see Renderer.ShowFPS), because a
// timestamp per timer is not free and a shipped frame should not pay for a HUD nobody
// is looking at; the GPU figures read zero until then.
//
// Beyond whatever timers its user starts, the renderer times each frame step (see
// Frame.Profiler) and these parts of the frame on the GPU, none of them within another:
//
//   - "cull": the compute cull for every view, plus skinning and particle simulation —
//     everything recorded before the first render pass.
//   - "shadow": the depth passes that fill the shadow maps, all lights and all cascades.
//   - "clusters": listing each light cluster's point and spot lights, and filling the
//     volumetric fog's volume.
//   - "prepass": the depth-only pass before forward shading; only with
//     Renderer.EnableDepthPrepass.
//   - "opaque": every opaque surface shaded.
//   - "occlusion": measuring ambient occlusion, and darkening the scene by it when that
//     needs a pass of its own.
//   - "transparent": everything drawn over the opaque scene — the backgrounds, the
//     blended surfaces and particles, the frame steps between them, and resolving the
//     multisampled scene.
//   - "post": an HDR frame's post-processing chain and its tone map; only with HDR.
//
// The zero value is ready to use.
type Profiler struct {
	// cpuTimers are the CPU timers, in the order their names were first started.
	cpuTimers       []cpuTimer
	cpuTimersByName map[string]CPUTimer

	// Wall-clock, CPU and wait times, as rings over the last profileSamples frames.
	frameTimes [profileSamples]time.Duration
	cpuTimes   [profileSamples]time.Duration
	waitTimes  [waitCount][profileSamples]time.Duration
	frameCount int
	frameStart time.Time
	// waitStart is when each wait in progress began; zero when it is not in progress.
	waitStart [waitCount]time.Time

	timestampPool gpu.QueryPool

	// gpuTimers are the GPU timers, in the order their names were first started.
	// gpuTimerSpans is the timer of each span the frame started, in order: the i-th has
	// its timestamps at gpuTimerStartSlot(i), when i is within the pool's
	// gpuTimerSpanCapacity.
	gpuTimers            []gpuTimer
	gpuTimersByName      map[string]GPUTimer
	gpuTimerSpans        []GPUTimer
	gpuTimerSpanCapacity int

	// The frame's smoothed GPU time, and how many frames' timestamps have been read back.
	gpuFrameMS    float64
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

// StartGPUTimer starts timing the GPU work cmd records next, until StopGPUTimer. The
// first start of a name makes its timer, and every later start of the name returns the
// same one; timers may run inside one another. Starting a timer that is already started
// does nothing. A timer's time is part of every timer it runs within.
//
// It measures nothing while GPU profiling is off (see Renderer.ShowFPS), and a timer
// left running when the frame ends stops there.
func (p *Profiler) StartGPUTimer(name string, cmd gpu.CommandBuffer) GPUTimer {
	timer := p.gpuTimerNamed(name)
	t := &p.gpuTimers[timer.index]
	if !p.isGPUProfilingEnabled() || t.hasOpenSpan {
		return timer
	}
	t.openSpan, t.hasOpenSpan = len(p.gpuTimerSpans), true
	p.gpuTimerSpans = append(p.gpuTimerSpans, timer)
	// Neither end may use StageNone: that resolves to the command buffer's start, so
	// every timer would appear to begin when the frame did, and the later ones would
	// each measure almost the whole frame. Any real stage samples where the write sits.
	if t.openSpan < p.gpuTimerSpanCapacity {
		cmd.WriteTimestamp(p.timestampPool, gpuTimerStartSlot(t.openSpan), gpu.StageCompute)
	}
	return timer
}

// StopGPUTimer stops a timer StartGPUTimer started. Stopping a timer that is not
// started does nothing.
func (p *Profiler) StopGPUTimer(timer GPUTimer, cmd gpu.CommandBuffer) {
	t := &p.gpuTimers[timer.index]
	if !t.hasOpenSpan {
		return
	}
	t.hasOpenSpan = false
	if t.openSpan < p.gpuTimerSpanCapacity {
		cmd.WriteTimestamp(p.timestampPool, gpuTimerEndSlot(t.openSpan), gpu.StageAll)
	}
}

// GPUTimings lists every GPU timer, in the order their names were first started. Their
// times are zero while GPU profiling is off (see Renderer.ShowFPS).
//
// A timer can run and still measure zero, which is why IsRecorded is not Time > 0.
// Whether a timestamp resolves where it was written is the backend's business, and not
// every backend can resolve one everywhere. KosmicKrisp samples only at render-pass
// boundaries, so a write made outside one is deferred to the next pass's start — which
// collapses the two ends of compute-only work onto the same tick, and the cull measures
// exactly zero there. It ran; it cannot be timed.
func (p *Profiler) GPUTimings() []Timing {
	timings := make([]Timing, len(p.gpuTimers))
	for i := range p.gpuTimers {
		t := &p.gpuTimers[i]
		timings[i] = Timing{Name: t.name, Time: durationFromMS(t.averageMS), IsRecorded: t.isRecorded}
	}
	return timings
}

// gpuTimerNamed returns the GPU timer named name, making it the first time.
func (p *Profiler) gpuTimerNamed(name string) GPUTimer {
	if timer, ok := p.gpuTimersByName[name]; ok {
		return timer
	}
	if p.gpuTimersByName == nil {
		p.gpuTimersByName = map[string]GPUTimer{}
	}
	timer := GPUTimer{index: len(p.gpuTimers)}
	p.gpuTimers = append(p.gpuTimers, gpuTimer{name: name})
	p.gpuTimersByName[name] = timer
	return timer
}

// StartCPUTimer starts timing the CPU work that follows, until StopCPUTimer. The first
// start of a name makes its timer, and every later start of the name returns the same
// one. Starting a timer that is already started does nothing.
//
// Its time counts toward the frame Render ends next, wherever it was measured.
func (p *Profiler) StartCPUTimer(name string) CPUTimer {
	timer, ok := p.cpuTimersByName[name]
	if !ok {
		if p.cpuTimersByName == nil {
			p.cpuTimersByName = map[string]CPUTimer{}
		}
		timer = CPUTimer{index: len(p.cpuTimers)}
		p.cpuTimers = append(p.cpuTimers, cpuTimer{name: name})
		p.cpuTimersByName[name] = timer
	}
	if t := &p.cpuTimers[timer.index]; t.start.IsZero() {
		t.start = time.Now()
	}
	return timer
}

// StopCPUTimer stops a timer StartCPUTimer started. Stopping a timer that is not
// started does nothing.
func (p *Profiler) StopCPUTimer(timer CPUTimer) {
	t := &p.cpuTimers[timer.index]
	if t.start.IsZero() {
		return
	}
	t.pendingTime += time.Since(t.start)
	t.start = time.Time{}
}

// CPUTimings lists every CPU timer, in the order their names were first started.
func (p *Profiler) CPUTimings() []Timing {
	timings := make([]Timing, len(p.cpuTimers))
	for i := range p.cpuTimers {
		t := &p.cpuTimers[i]
		timings[i] = Timing{Name: t.name, Time: averageOfRing(&t.times, t.frameCount), IsRecorded: t.isRecorded}
	}
	return timings
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
	p.endCPUTimerFrames()
}

// endCPUTimerFrames moves what each CPU timer measured since the last frame ended into
// its ring.
func (p *Profiler) endCPUTimerFrames() {
	for i := range p.cpuTimers {
		t := &p.cpuTimers[i]
		t.frameCount++
		t.times[(t.frameCount-1)%profileSamples] = t.pendingTime
		t.isRecorded = t.pendingTime > 0
		t.pendingTime = 0
	}
}

// ringIndex is this frame's position in the CPU and frame-time rings.
func (p *Profiler) ringIndex() int {
	return (p.frameCount - 1) % profileSamples
}

// average is the mean of the filled part of a ring, so the first frames are not
// diluted by the still-zero tail.
func (p *Profiler) average(ring *[profileSamples]time.Duration) time.Duration {
	return averageOfRing(ring, p.frameCount)
}

// averageOfRing is the mean of the part of a ring that frameCount frames have filled.
func averageOfRing(ring *[profileSamples]time.Duration, frameCount int) time.Duration {
	n := min(frameCount, profileSamples)
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
	if p.timestampPool.IsValid() {
		return
	}
	p.gpuTimerSpanCapacity = max(p.gpuTimerSpanCapacity, initialGPUTimerSpans)
	p.timestampPool = backend.CreateTimestampPool(p.timestampSlotCount())
}

// timestampSlotCount is how many timestamps the pool holds.
func (p *Profiler) timestampSlotCount() uint32 {
	return gpuTimerStartSlot(p.gpuTimerSpanCapacity)
}

// disableGPUProfiling drops the timestamp pool, which turns GPU profiling off. The
// figures already gathered are kept, but a timer that stops being measured is not one
// that stopped running, so no timer is reported as recorded any more.
func (p *Profiler) disableGPUProfiling(backend gpu.Backend) {
	if p.timestampPool.IsValid() {
		backend.DestroyTimestampPool(p.timestampPool)
	}
	p.timestampPool = gpu.QueryPool{}
	p.gpuTimerSpans = p.gpuTimerSpans[:0]
	for i := range p.gpuTimers {
		p.gpuTimers[i].hasOpenSpan = false
		p.gpuTimers[i].isRecorded = false
	}
}

// beginGPUFrame resets the pool and stamps the frame's start. Every slot is cleared, so
// none reads back as last frame's value.
//
// The start uses StageNone, which is not "no particular stage": it means the command
// buffer's own start, resolved from the submission's GPU boundaries. That is exactly
// what the frame total wants, and exactly what a timer must never use — see
// StartGPUTimer.
func (p *Profiler) beginGPUFrame(cmd gpu.CommandBuffer) {
	if !p.isGPUProfilingEnabled() {
		return
	}
	cmd.ResetTimestamps(p.timestampPool, p.timestampSlotCount())
	cmd.WriteTimestamp(p.timestampPool, frameStartSlot, gpu.StageNone)
	p.gpuTimerSpans = p.gpuTimerSpans[:0]
}

// stopOpenGPUTimers stops every GPU timer still running, so that none of the frame's
// spans is left without its end. Required rather than tidy: a backend may read the pool
// by waiting for all of its results, and a query that was reset but never written never
// becomes available — leaving one unwritten hangs that read forever.
//
// It must be recorded while the frame's last render pass is still open; see
// encodeOverlay for why that placement is load-bearing.
func (p *Profiler) stopOpenGPUTimers(cmd gpu.CommandBuffer) {
	for i := range p.gpuTimers {
		p.StopGPUTimer(GPUTimer{index: i}, cmd)
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
	// Only the slots this frame wrote: a timer slot past the spans the frame started was
	// reset and never written, and a backend may wait forever for it (see
	// stopOpenGPUTimers).
	timedSpans := min(len(p.gpuTimerSpans), p.gpuTimerSpanCapacity)
	timestamps := backend.ReadTimestamps(p.timestampPool, gpuTimerStartSlot(timedSpans))
	p.growTimestampPoolToFitSpans(backend)
	p.recordWhichGPUTimersRan()
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

	p.readGPUTimerTimes(elapsedMS, firstSample)
}

// growTimestampPoolToFitSpans makes the pool big enough for every span the frame
// started, for the frames after: one that started more had the rest go untimed.
func (p *Profiler) growTimestampPoolToFitSpans(backend gpu.Backend) {
	if len(p.gpuTimerSpans) <= p.gpuTimerSpanCapacity {
		return
	}
	backend.DestroyTimestampPool(p.timestampPool)
	p.gpuTimerSpanCapacity = len(p.gpuTimerSpans) * 2
	p.timestampPool = backend.CreateTimestampPool(p.timestampSlotCount())
}

// recordWhichGPUTimersRan marks the timers the frame started as recorded, and every
// other as not — from the recording, so that it holds while the times are still
// warming up, and when they cannot be read.
func (p *Profiler) recordWhichGPUTimersRan() {
	for i := range p.gpuTimers {
		p.gpuTimers[i].isRecorded = false
	}
	for _, timer := range p.gpuTimerSpans {
		p.gpuTimers[timer.index].isRecorded = true
	}
}

// readGPUTimerTimes folds each GPU timer's spans this frame into its average. A timer
// the frame did not run has no time; one it ran but could not measure keeps its last,
// rather than averaging toward zero. Whether a timer ran is known from the recording,
// not inferred from its timestamps (see GPUTimings).
func (p *Profiler) readGPUTimerTimes(elapsedMS func(startSlot, endSlot uint32) (float64, bool), firstSample bool) {
	for i := range p.gpuTimers {
		t := &p.gpuTimers[i]
		t.frameMeasuredSpans, t.frameMS = 0, 0
	}
	for i, timer := range p.gpuTimerSpans {
		t := &p.gpuTimers[timer.index]
		if i >= p.gpuTimerSpanCapacity {
			continue
		}
		if ms, ok := elapsedMS(gpuTimerStartSlot(i), gpuTimerEndSlot(i)); ok {
			t.frameMeasuredSpans++
			t.frameMS += ms
		}
	}
	for i := range p.gpuTimers {
		t := &p.gpuTimers[i]
		if !t.isRecorded {
			t.averageMS = 0
			continue
		}
		// A timer that was not running last frame has no average to continue.
		if t.frameMeasuredSpans > 0 {
			t.averageMS = smooth(t.averageMS, t.frameMS, firstSample || t.averageMS == 0)
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
