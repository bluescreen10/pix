package scenes

import "math"

// Float32 math helpers. The scene graph needs a handful of these and nothing more, so
// they live here rather than pulling in a dependency for three one-line functions.

func sqrt32(x float32) float32 { return float32(math.Sqrt(float64(x))) }

func abs32(x float32) float32 {
	if x < 0 {
		return -x
	}
	return x
}
