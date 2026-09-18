// Package particles supplies emitter and spawner helpers for pix's particle system.
// Core types (Particle, ParticleContainer, ParticleEmitter, ParticleSpawner) live in
// scenes; this package imports scenes, never the other way around. Built-in per-particle
// update behavior is configured directly on scenes.ParticleUpdate, not composed from
// constructors here — it runs as a GPU kernel, not Go code, so there is nothing to
// construct at this layer for it.
package particles

import "github.com/bluescreen10/pix/scenes"

// SpawnFunc adapts a plain function to scenes.ParticleSpawner.
type SpawnFunc func(*scenes.Particle)

func (f SpawnFunc) Spawn(p *scenes.Particle) { f(p) }

// RateEmitter requests births at a steady rate, accumulating fractional births
// between updates so a rate like 0.5/s still produces exactly one birth every two
// seconds rather than never emitting or rounding every step.
type RateEmitter struct {
	perSecond float64
	acc       float64
}

// Rate creates a fresh RateEmitter. perSecond must be nonnegative and finite.
func Rate(perSecond float64) *RateEmitter {
	if perSecond < 0 {
		panic("particles: Rate requires a nonnegative rate")
	}
	return &RateEmitter{perSecond: perSecond}
}

func (e *RateEmitter) Emit(dt float32) int {
	e.acc += e.perSecond * float64(dt)
	n := int(e.acc)
	e.acc -= float64(n)
	return n
}

func (e *RateEmitter) Reset() { e.acc = 0 }

// BurstEmitter requests its full count once, on the first positive Emit call, and
// nothing afterward until Reset rearms it.
type BurstEmitter struct {
	count int
	armed bool
}

// Burst creates a fresh BurstEmitter. count must be nonnegative.
func Burst(count int) *BurstEmitter {
	if count < 0 {
		panic("particles: Burst requires a nonnegative count")
	}
	return &BurstEmitter{count: count, armed: true}
}

func (e *BurstEmitter) Emit(dt float32) int {
	if !e.armed || dt <= 0 {
		return 0
	}
	e.armed = false
	return e.count
}

func (e *BurstEmitter) Reset() { e.armed = true }
