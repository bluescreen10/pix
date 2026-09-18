package particles_test

import (
	"testing"

	"github.com/bluescreen10/pix/particles"
	"github.com/bluescreen10/pix/scenes"
)

func TestRateAccumulatesFractionalBirths(t *testing.T) {
	e := particles.Rate(1) // one per second
	// Half a second at a time: nothing until the second half-step crosses 1.
	if n := e.Emit(0.5); n != 0 {
		t.Fatalf("Emit(0.5) = %d, want 0 (0.5 births accumulated)", n)
	}
	if n := e.Emit(0.5); n != 1 {
		t.Fatalf("Emit(0.5) = %d, want 1 (accumulator reached 1.0)", n)
	}
	if n := e.Emit(0.5); n != 0 {
		t.Fatalf("Emit(0.5) = %d, want 0 (carried the leftover 0.5)", n)
	}
}

func TestRateDoesNotEmitAtConstruction(t *testing.T) {
	e := particles.Rate(1) // one per second
	if n := e.Emit(0.999999); n != 0 {
		t.Fatalf("first Emit(0.999999) = %d, want 0 (no birth accumulated before construction)", n)
	}
}

func TestRateZeroNeverEmits(t *testing.T) {
	e := particles.Rate(0)
	for i := 0; i < 100; i++ {
		if n := e.Emit(1); n != 0 {
			t.Fatalf("Emit with rate 0 = %d, want 0", n)
		}
	}
}

func TestRateNegativePanics(t *testing.T) {
	defer func() {
		if recover() == nil {
			t.Fatal("expected panic for a negative rate")
		}
	}()
	particles.Rate(-1)
}

func TestRateReset(t *testing.T) {
	e := particles.Rate(1)
	e.Emit(0.9) // 0.9 accumulated, not yet a birth
	e.Reset()
	if n := e.Emit(0.5); n != 0 {
		t.Fatalf("Emit(0.5) after Reset = %d, want 0 (accumulator was cleared, not carried)", n)
	}
}

func TestBurstRequestsOnceOnFirstPositiveUpdate(t *testing.T) {
	e := particles.Burst(50)
	if n := e.Emit(1.0 / 60); n != 50 {
		t.Fatalf("first Emit = %d, want 50", n)
	}
	if n := e.Emit(1.0 / 60); n != 0 {
		t.Fatalf("second Emit = %d, want 0 (burst already fired)", n)
	}
}

func TestBurstIgnoresNonPositiveDt(t *testing.T) {
	e := particles.Burst(10)
	if n := e.Emit(0); n != 0 {
		t.Fatalf("Emit(0) = %d, want 0", n)
	}
	if n := e.Emit(1.0 / 60); n != 10 {
		t.Fatalf("first positive Emit after Emit(0) = %d, want 10 (burst was not consumed)", n)
	}
}

func TestBurstNegativeCountPanics(t *testing.T) {
	defer func() {
		if recover() == nil {
			t.Fatal("expected panic for a negative count")
		}
	}()
	particles.Burst(-1)
}

func TestBurstReset(t *testing.T) {
	e := particles.Burst(5)
	e.Emit(1.0 / 60) // fires once
	e.Reset()
	if n := e.Emit(1.0 / 60); n != 5 {
		t.Fatalf("Emit after Reset = %d, want 5 (rearmed)", n)
	}
}

func TestSpawnFuncAdapter(t *testing.T) {
	var got scenes.Particle
	var s scenes.ParticleSpawner = particles.SpawnFunc(func(p *scenes.Particle) {
		p.Lifetime = 3
		got = *p
	})
	p := scenes.Particle{}
	s.Spawn(&p)
	if got.Lifetime != 3 {
		t.Fatalf("SpawnFunc did not forward the call: got.Lifetime = %v, want 3", got.Lifetime)
	}
	if p.Lifetime != 3 {
		t.Fatalf("Spawn must edit the particle in place: p.Lifetime = %v, want 3", p.Lifetime)
	}
}
