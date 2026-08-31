package vulkan

import (
	"testing"

	"github.com/bluescreen10/pix/gpu"
)

func TestBindless(t *testing.T) {
	b := New()
	if err := b.Init(); err != nil {
		t.Skipf("vulkan device unavailable: %v", err)
	}
	defer b.Destroy()

	s, st, sa := b.HeapCapacities()
	t.Logf("heap caps: sampled=%d storage=%d sampler=%d", s, st, sa)
	if s == 0 || st == 0 || sa == 0 {
		t.Fatal("zero heap capacity")
	}

	a := b.CreateSampler(gpu.SamplerDescriptor{MagLinear: true, MinLinear: true, MipLinear: true})
	c := b.CreateSampler(gpu.SamplerDescriptor{Compare: gpu.CompareLessEqual}) // shadow comparison sampler
	if a.Index != 0 || c.Index != 1 {
		t.Fatalf("unexpected sampler indices: %d %d", a.Index, c.Index)
	}
	t.Logf("samplers registered at heap indices %d, %d", a.Index, c.Index)
	b.DestroySampler(a)
	b.DestroySampler(c)
}
