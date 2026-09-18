package scenes

import (
	"math"
	"testing"

	"github.com/bluescreen10/pix/colors"
)

// TestFogPacking pins the packed form the shaders read: mode in fogColor.w and
// (near, far, density) in fogParams, with nil meaning no fog.
// exp2Density mirrors Exp2Fog.fogState's arithmetic: the constant folds to float32
// before the divide, which rounds differently from float32(const / const).
func exp2Density(distance float32) float32 { return exp2DensityScale / distance }

func TestFogPacking(t *testing.T) {
	tests := []struct {
		name   string
		fog    Fog
		color  colors.RGB32F
		mode   uint32
		params [3]float32
	}{
		{"nil disables", nil, colors.RGB32F{}, FogNone, [3]float32{}},
		{"linear", NewLinearFog(colors.RGB32F{0.5, 0.6, 0.7}, 10, 200), colors.RGB32F{0.5, 0.6, 0.7}, FogLinear, [3]float32{10, 200, 0}},
		{"exp2", NewExp2Fog(colors.RGB32F{0.1, 0.2, 0.3}, 1000), colors.RGB32F{0.1, 0.2, 0.3}, FogExp2, [3]float32{0, 0, exp2Density(1000)}},
		{"exp2 zero distance disables", NewExp2Fog(colors.RGB32F{1, 1, 1}, 0), colors.RGB32F{}, FogNone, [3]float32{}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			s := StateOf(tc.fog)
			if s.Color != tc.color || s.Mode != tc.mode {
				t.Errorf("color/mode = %v/%d, want %v/%d", s.Color, s.Mode, tc.color, tc.mode)
			}
			if got := [3]float32{s.Near, s.Far, s.Density}; got != tc.params {
				t.Errorf("params = %v, want %v", got, tc.params)
			}
		})
	}
}

// TestExp2FogDistanceMeaning checks that Distance means what it claims: a surface at
// exactly that distance is ~10% visible, and one much nearer is barely touched. This
// is the whole point of the parameterization, so it is worth pinning against the
// shader's exp(-(d*density)^2).
func TestExp2FogDistanceMeaning(t *testing.T) {
	const dist = 800
	density := float64(NewExp2Fog(colors.RGB32F{}, dist).fogState().Density)
	transmittance := func(d float64) float64 {
		t := d * density
		return math.Exp(-t * t)
	}
	if got := transmittance(dist); math.Abs(got-exp2VisibleAtDistance) > 1e-5 {
		t.Errorf("visibility at Distance = %.5f, want %.2f", got, exp2VisibleAtDistance)
	}
	// A tenth of the way out should still be almost entirely clear — the squared
	// exponent is what buys this over a plain exponential.
	if got := transmittance(dist / 10); got < 0.97 {
		t.Errorf("visibility at Distance/10 = %.4f, want > 0.97 (foreground should stay clear)", got)
	}
}
