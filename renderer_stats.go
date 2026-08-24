package pix

import "time"

type RendererStats struct {
	frameTimes []float64
	cpuTimes   []float64
	gpuEMA     float64 // GPU time (seconds), smoothed — readback is sparse/async

	currentFrame int
	maxSamples   int

	start time.Time
}

func NewRendererStats(maxSamples int) *RendererStats {
	return &RendererStats{
		frameTimes: make([]float64, maxSamples),
		cpuTimes:   make([]float64, maxSamples),
		maxSamples: maxSamples,
	}
}

func (s *RendererStats) StartFrame() {
	s.currentFrame++
	s.start = time.Now()

}

func (s *RendererStats) EndFrame() {
	frameTime := time.Since(s.start).Seconds()
	s.frameTimes[s.currentFrame%s.maxSamples] = frameTime
}

// AddGPUTime records a GPU frame time (seconds) from the async timestamp
// readback, smoothed into an EMA because samples arrive sparsely.
func (s *RendererStats) AddGPUTime(gpuTime float64) {
	if s.gpuEMA == 0 {
		s.gpuEMA = gpuTime
	} else {
		s.gpuEMA = s.gpuEMA*0.9 + gpuTime*0.1
	}
}

// AddCPUTime records the CPU time spent producing this frame (list building,
// culling, command encoding and submit) — excluding the vsync-blocking present.
func (s *RendererStats) AddCPUTime(d time.Duration) {
	s.cpuTimes[s.currentFrame%s.maxSamples] = d.Seconds()
}

// AvgCPUTime is the rolling average CPU frame cost.
func (s *RendererStats) AvgCPUTime() time.Duration {
	var total float64
	for _, t := range s.cpuTimes {
		total += t
	}
	return time.Duration(total / float64(s.maxSamples) * float64(time.Second))
}

func (s *RendererStats) FPS() float64 {
	return 1 / float64(s.AvgFrameTime().Seconds())
}

func (s *RendererStats) AvgFrameTime() time.Duration {
	var total float64

	for _, ft := range s.frameTimes {
		total += ft
	}

	return time.Duration(total / float64(s.maxSamples) * float64(time.Second))
}

func (s *RendererStats) AvgGPUTime() time.Duration {
	return time.Duration(s.gpuEMA * float64(time.Second))
}
