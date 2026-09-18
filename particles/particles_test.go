package particles

import (
	"testing"

	"github.com/bluescreen10/pix/scenes"
)

func TestRateAccumulatesFractionalBirths(t *testing.T) {
	e := Rate(1) // one per second
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
	e := Rate(1000)
	if e.acc != 0 {
		t.Fatalf("accumulator after construction = %v, want 0 (nothing emitted yet)", e.acc)
	}
}

func TestRateZeroNeverEmits(t *testing.T) {
	e := Rate(0)
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
	Rate(-1)
}

func TestRateReset(t *testing.T) {
	e := Rate(1)
	e.Emit(0.9) // 0.9 accumulated, not yet a birth
	e.Reset()
	if n := e.Emit(0.5); n != 0 {
		t.Fatalf("Emit(0.5) after Reset = %d, want 0 (accumulator was cleared, not carried)", n)
	}
}

func TestBurstRequestsOnceOnFirstPositiveUpdate(t *testing.T) {
	e := Burst(50)
	if n := e.Emit(1.0 / 60); n != 50 {
		t.Fatalf("first Emit = %d, want 50", n)
	}
	if n := e.Emit(1.0 / 60); n != 0 {
		t.Fatalf("second Emit = %d, want 0 (burst already fired)", n)
	}
}

func TestBurstDoesNotEmitAtConstruction(t *testing.T) {
	e := Burst(10)
	if !e.armed {
		t.Fatal("Burst must not fire until the first positive Emit call")
	}
}

func TestBurstIgnoresNonPositiveDt(t *testing.T) {
	e := Burst(10)
	if n := e.Emit(0); n != 0 {
		t.Fatalf("Emit(0) = %d, want 0", n)
	}
	if !e.armed {
		t.Fatal("Emit(0) must not consume the burst")
	}
	if n := e.Emit(1.0 / 60); n != 10 {
		t.Fatalf("first positive Emit = %d, want 10", n)
	}
}

func TestBurstNegativeCountPanics(t *testing.T) {
	defer func() {
		if recover() == nil {
			t.Fatal("expected panic for a negative count")
		}
	}()
	Burst(-1)
}

func TestBurstReset(t *testing.T) {
	e := Burst(5)
	e.Emit(1.0 / 60) // fires once
	e.Reset()
	if n := e.Emit(1.0 / 60); n != 5 {
		t.Fatalf("Emit after Reset = %d, want 5 (rearmed)", n)
	}
}

func TestSpawnFuncAdapter(t *testing.T) {
	var got scenes.Particle
	var s scenes.ParticleSpawner = SpawnFunc(func(p *scenes.Particle) {
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
