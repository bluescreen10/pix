package pix

import (
	"math"
	"testing"
	"unsafe"

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
		{"nil disables", nil, colors.RGB32F{}, fogNone, [3]float32{}},
		{"linear", NewLinearFog(colors.RGB32F{0.5, 0.6, 0.7}, 10, 200), colors.RGB32F{0.5, 0.6, 0.7}, fogLinear, [3]float32{10, 200, 0}},
		{"exp2", NewExp2Fog(colors.RGB32F{0.1, 0.2, 0.3}, 1000), colors.RGB32F{0.1, 0.2, 0.3}, fogExp2, [3]float32{0, 0, exp2Density(1000)}},
		{"exp2 zero distance disables", NewExp2Fog(colors.RGB32F{1, 1, 1}, 0), colors.RGB32F{}, fogNone, [3]float32{}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			s := stateOf(tc.fog)
			if s.color != tc.color || s.mode != tc.mode {
				t.Errorf("color/mode = %v/%d, want %v/%d", s.color, s.mode, tc.color, tc.mode)
			}
			if got := [3]float32{s.near, s.far, s.density}; got != tc.params {
				t.Errorf("params = %v, want %v", got, tc.params)
			}
		})
	}
}

// TestFogLightTableLayout guards the offsets LightBuf in lighting.glsl depends on:
// fog sits between ambient and the light counts, so a field added above it silently
// desynchronizes every lit shader.
func TestFogLightTableLayout(t *testing.T) {
	var g gpuLights
	for _, c := range []struct {
		name string
		got  uintptr
		want uintptr
	}{
		{"ambient", unsafe.Offsetof(g.ambient), 0},
		{"fogColor", unsafe.Offsetof(g.fogColor), 16},
		{"fogParams", unsafe.Offsetof(g.fogParams), 32},
		{"numDir", unsafe.Offsetof(g.numDir), 48},
	} {
		if c.got != c.want {
			t.Errorf("offset of %s = %d, want %d", c.name, c.got, c.want)
		}
	}
}

// TestFogRebuild checks that fog reaches the GPU table and that changing it alone
// re-dirties the buffer — the table is only re-uploaded when rebuild sees a change.
func TestFogRebuild(t *testing.T) {
	l := &Lights{}
	l.rebuild(colors.RGB32F{}, nil, nil, nil, nil, false)
	if l.data.fogColor[3] != float32(fogNone) {
		t.Fatalf("nil fog wrote mode %v, want %d", l.data.fogColor[3], fogNone)
	}
	l.dirty = false

	fog := NewExp2Fog(colors.RGB32F{0.4, 0.5, 0.6}, 500)
	l.rebuild(colors.RGB32F{}, fog, nil, nil, nil, false)
	if !l.dirty {
		t.Error("setting fog did not dirty the table")
	}
	if l.data.fogColor != [4]float32{0.4, 0.5, 0.6, float32(fogExp2)} {
		t.Errorf("fogColor = %v", l.data.fogColor)
	}
	if want := exp2Density(500); l.data.fogParams[2] != want {
		t.Errorf("density = %v, want %v", l.data.fogParams[2], want)
	}

	// Mutating the fog object in place must be picked up: the fields are exported
	// precisely so they can be animated.
	l.dirty = false
	fog.Distance = 200
	l.rebuild(colors.RGB32F{}, fog, nil, nil, nil, false)
	if want := exp2Density(200); !l.dirty || l.data.fogParams[2] != want {
		t.Errorf("in-place distance change not picked up: dirty=%v params=%v", l.dirty, l.data.fogParams)
	}
}

// TestExp2FogDistanceMeaning checks that Distance means what it claims: a surface at
// exactly that distance is ~10% visible, and one much nearer is barely touched. This
// is the whole point of the parameterization, so it is worth pinning against the
// shader's exp(-(d*density)^2).
func TestExp2FogDistanceMeaning(t *testing.T) {
	const dist = 800
	density := float64(NewExp2Fog(colors.RGB32F{}, dist).fogState().density)
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
