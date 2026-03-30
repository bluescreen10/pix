package pix

import "time"

type RendererStats struct {
	frameTimes []float64
	gpuTimes   []float64

	currentFrame int
	maxSamples   int
}

func NewRendererStats(maxSamples int) *RendererStats {
	return &RendererStats{
		frameTimes: make([]float64, maxSamples),
		gpuTimes:   make([]float64, maxSamples),
		maxSamples: maxSamples,
	}
}

func (s *RendererStats) AddFrameTime(frameTime float64) {
	s.frameTimes[s.currentFrame%s.maxSamples] = frameTime
}

func (s *RendererStats) AddGPUTime(gpuTime float64) {
	s.gpuTimes[s.currentFrame%s.maxSamples] = gpuTime
}

func (s *RendererStats) NextFrame() {
	s.currentFrame++
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
	var total float64

	for _, gt := range s.gpuTimes {
		total += gt
	}

	return time.Duration(total / float64(s.maxSamples) * float64(time.Second))
}
