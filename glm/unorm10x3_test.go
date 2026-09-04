package glm

import (
	"math"
	"testing"
)

// quantStep is one 10-bit step: the channel spans [0,1] over 1023 steps.
const quantStep = 1.0 / 1023.0

func TestUnorm10x3RoundTrip(t *testing.T) {
	for _, v := range []Vec3f{
		{0, 0, 0}, {1, 1, 1}, {0.5, 0.25, 0.75}, {1, 0, 0}, {0, 1, 0}, {0, 0, 1},
		{0.333, 0.666, 0.999},
	} {
		got := v.Unorm10x3().Vec3f()
		for i := range got {
			if d := float32(math.Abs(float64(got[i] - v[i]))); d > quantStep {
				t.Errorf("%v round-tripped to %v, channel %d off by %g (> one step %g)", v, got, i, d, quantStep)
			}
		}
	}
}

// TestUnorm10x3Unbiased is the reason the pack rounds instead of truncating:
// truncation shifts every channel down by half a step on average, which on a normal
// is a systematic tilt rather than noise.
func TestUnorm10x3Unbiased(t *testing.T) {
	var sum float64
	const n = 2001
	for i := 0; i < n; i++ {
		v := float32(i) / float32(n-1)
		sum += float64(Vec3f{v, v, v}.Unorm10x3().Vec3f()[0] - v)
	}
	if mean := sum / n; math.Abs(mean) > quantStep/8 {
		t.Errorf("mean round-trip error = %g, want ~0 (truncation would give about -%g)", mean, quantStep/2)
	}
}

func TestUnorm10x3Channels(t *testing.T) {
	// Each channel must land in its own 10-bit lane, and the top 2 bits stay clear.
	if p := (Vec3f{1, 0, 0}).Unorm10x3(); p != Unorm10x3(1023) {
		t.Errorf("{1,0,0} packed to %#x, want 0x3ff", uint32(p))
	}
	if p := (Vec3f{0, 0, 1}).Unorm10x3(); p != Unorm10x3(1023)<<20 {
		t.Errorf("{0,0,1} packed to %#x, want 0x3ff00000", uint32(p))
	}
	if p := (Vec3f{1, 1, 1}).Unorm10x3(); uint32(p)>>30 != 0 {
		t.Errorf("top 2 bits set: %#x", uint32(p))
	}
	// Out-of-range input clamps rather than wrapping into a neighbouring channel.
	if p := (Vec3f{2, -1, 0}).Unorm10x3(); p != Unorm10x3(1023) {
		t.Errorf("{2,-1,0} packed to %#x, want 0x3ff (clamped)", uint32(p))
	}
}

// TestUnorm10x3NormalRemap covers the signed remap geometry.go applies around this
// storage, and that scene_draw.vert.glsl inverts — the pack site is the only Go half.
func TestUnorm10x3NormalRemap(t *testing.T) {
	inv := float32(1 / math.Sqrt(3))
	for _, n := range []Vec3f{{0, 0, 1}, {0, 0, -1}, {-1, 0, 0}, {inv, inv, inv}, {0.6, -0.8, 0}} {
		p := Vec3f{n[0]*0.5 + 0.5, n[1]*0.5 + 0.5, n[2]*0.5 + 0.5}.Unorm10x3().Vec3f()
		got := Vec3f{p[0]*2 - 1, p[1]*2 - 1, p[2]*2 - 1}
		for i := range got {
			if d := float32(math.Abs(float64(got[i] - n[i]))); d > 2*quantStep {
				t.Errorf("normal %v round-tripped to %v, channel %d off by %g", n, got, i, d)
			}
		}
	}
}
