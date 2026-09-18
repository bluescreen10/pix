package scenes_test

import (
	"math"
	"testing"

	"github.com/bluescreen10/pix/colors"
	"github.com/bluescreen10/pix/scenes"
)

// TestFogPacking pins the packed form the shaders read: mode in fogColor.w and
// (near, far) in fogParams, with nil meaning no fog. The exp2 case (which also
// carries a Density) is checked separately in TestExp2FogPacksDensity.
func TestFogPacking(t *testing.T) {
	tests := []struct {
		name  string
		fog   scenes.Fog
		color colors.RGB32F
		mode  uint32
		near  float32
		far   float32
	}{
		{"nil disables", nil, colors.RGB32F{}, scenes.FogNone, 0, 0},
		{"linear", scenes.NewLinearFog(colors.RGB32F{0.5, 0.6, 0.7}, 10, 200), colors.RGB32F{0.5, 0.6, 0.7}, scenes.FogLinear, 10, 200},
		{"exp2 zero distance disables", scenes.NewExp2Fog(colors.RGB32F{1, 1, 1}, 0), colors.RGB32F{}, scenes.FogNone, 0, 0},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			s := scenes.StateOf(tc.fog)
			if s.Color != tc.color || s.Mode != tc.mode {
				t.Errorf("color/mode = %v/%d, want %v/%d", s.Color, s.Mode, tc.color, tc.mode)
			}
			if s.Near != tc.near || s.Far != tc.far {
				t.Errorf("near/far = %v/%v, want %v/%v", s.Near, s.Far, tc.near, tc.far)
			}
			if s.Density != 0 {
				t.Errorf("density = %v, want 0 (this case carries no density)", s.Density)
			}
		})
	}
}

// TestExp2FogPacksDensity checks the exp2 case: Near/Far are unused for this mode,
// and Density follows the formula documented on Exp2Fog (10% visible at Distance),
// checked to a loose tolerance since the exact bit pattern is an implementation
// detail rather than part of the public contract.
func TestExp2FogPacksDensity(t *testing.T) {
	s := scenes.StateOf(scenes.NewExp2Fog(colors.RGB32F{0.1, 0.2, 0.3}, 1000))
	if want := (colors.RGB32F{0.1, 0.2, 0.3}); s.Color != want || s.Mode != scenes.FogExp2 {
		t.Fatalf("color/mode = %v/%d, want %v/%d", s.Color, s.Mode, want, scenes.FogExp2)
	}
	if s.Near != 0 || s.Far != 0 {
		t.Fatalf("near/far = %v/%v, want 0/0 (exp2 doesn't use them)", s.Near, s.Far)
	}
	want := float32(math.Sqrt(-math.Log(0.1))) / 1000 // documented: 10% visible at Distance
	if math.Abs(float64(s.Density-want)) > 1e-4 {
		t.Fatalf("density = %v, want ~%v", s.Density, want)
	}
}

// TestExp2FogDistanceMeaning checks that Distance means what it claims: a surface at
// exactly that distance is ~10% visible, and one much nearer is barely touched. This
// is the whole point of the parameterization, so it is worth pinning against the
// shader's exp(-(d*density)^2).
func TestExp2FogDistanceMeaning(t *testing.T) {
	const dist = 800
	const visibleAtDistance = 0.1
	density := float64(scenes.StateOf(scenes.NewExp2Fog(colors.RGB32F{}, dist)).Density)
	transmittance := func(d float64) float64 {
		t := d * density
		return math.Exp(-t * t)
	}
	if got := transmittance(dist); math.Abs(got-visibleAtDistance) > 1e-5 {
		t.Errorf("visibility at Distance = %.5f, want %.2f", got, visibleAtDistance)
	}
	// A tenth of the way out should still be almost entirely clear — the squared
	// exponent is what buys this over a plain exponential.
	if got := transmittance(dist / 10); got < 0.97 {
		t.Errorf("visibility at Distance/10 = %.4f, want > 0.97 (foreground should stay clear)", got)
	}
}
