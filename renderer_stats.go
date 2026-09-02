package pix

import "time"

// gpuWarmup is the number of initial GPU samples to discard before seeding the EMA.
// The first frames include one-off pipeline/shader compilation, which would
// otherwise dominate the smoothed value for a second or two.
const gpuWarmup = 3

type RendererStats struct {
	frameTimes []float64
	cpuTimes   []float64
	gpuEMA     float64 // GPU time (seconds), smoothed — readback is sparse/async
	gpuSeen    int     // GPU samples observed (to skip warmup, then seed the EMA)

	currentFrame int
	samples      int // frames recorded, capped at maxSamples
	maxSamples   int

	start time.Time
}

func newRendererStats(maxSamples int) *RendererStats {
	return &RendererStats{
		frameTimes: make([]float64, maxSamples),
		cpuTimes:   make([]float64, maxSamples),
		maxSamples: maxSamples,
	}
}

func (s *RendererStats) StartFrame() {
	s.currentFrame++
	if s.samples < s.maxSamples {
		s.samples++
	}
	s.start = time.Now()
}

// slot is the 0-based ring index for the current frame (frame 1 → slot 0).
func (s *RendererStats) slot() int { return (s.currentFrame - 1) % s.maxSamples }

func (s *RendererStats) EndFrame() {
	s.frameTimes[s.slot()] = time.Since(s.start).Seconds()
}

// AddGPUTime records a GPU frame time (seconds) from the timestamp readback,
// smoothed into an EMA. The first gpuWarmup samples are discarded so pipeline
// compilation on the opening frames doesn't skew the reading.
func (s *RendererStats) AddGPUTime(gpuTime float64) {
	s.gpuSeen++
	if s.gpuSeen <= gpuWarmup {
		return
	}
	if s.gpuEMA == 0 {
		s.gpuEMA = gpuTime
	} else {
		s.gpuEMA = s.gpuEMA*0.9 + gpuTime*0.1
	}
}

// AddCPUTime records the CPU time spent producing this frame (scene prep, culling
// and command encoding) — excluding the vsync-blocking acquire/present.
func (s *RendererStats) AddCPUTime(d time.Duration) {
	s.cpuTimes[s.slot()] = d.Seconds()
}

// AvgCPUTime is the rolling average CPU frame cost over the recorded samples.
func (s *RendererStats) AvgCPUTime() time.Duration {
	return s.avg(s.cpuTimes)
}

// AvgFrameTime is the rolling average wall-clock frame time.
func (s *RendererStats) AvgFrameTime() time.Duration {
	return s.avg(s.frameTimes)
}

// avg averages the first s.samples entries (so early frames aren't diluted by the
// still-zero tail of the ring buffer).
func (s *RendererStats) avg(buf []float64) time.Duration {
	if s.samples == 0 {
		return 0
	}
	var total float64
	for i := 0; i < s.samples; i++ {
		total += buf[i]
	}
	return time.Duration(total / float64(s.samples) * float64(time.Second))
}

func (s *RendererStats) FPS() float64 {
	ft := s.AvgFrameTime().Seconds()
	if ft <= 0 {
		return 0
	}
	return 1 / ft
}

func (s *RendererStats) AvgGPUTime() time.Duration {
	return time.Duration(s.gpuEMA * float64(time.Second))
}
