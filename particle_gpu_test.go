package pix

import (
	"math"
	"testing"
	"unsafe"

	"github.com/bluescreen10/pix/cameras"
	"github.com/bluescreen10/pix/glm"
)

// gpuParticleTestScene builds a renderer + scene + camera for tests that actually
// dispatch the update kernel and draw (unlike newParticleTestScene's CPU-only setup).
func gpuParticleTestScene(t *testing.T) (*Renderer, *Scene, Camera) {
	t.Helper()
	r, err := NewOffscreenRenderer(64, 64)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(r.Destroy)
	scene := r.NewScene()
	t.Cleanup(scene.Destroy)
	cam := cameras.NewPerspectiveCamera(45, 1, 0.1, 100)
	cam.SetPosition(glm.Vec3f{0, 0, 5})
	return r, scene, cam
}

// readParticles reads a container's current (last-compacted) buffer directly: the
// renderer's synchronous-per-frame design (Submit always Waits) means Render has
// returned only once the GPU has finished writing it, so this host-visible read is
// safe without any explicit readback step.
func readParticles(c ParticleContainer, n uint32) []particleRecord {
	d := c.data()
	buf := d.buffers[d.current]
	return unsafe.Slice((*particleRecord)(buf.Ptr), n)
}

func readIndirect(c ParticleContainer) indirectCmd {
	d := c.data()
	return *(*indirectCmd)(d.indirectBuf.Ptr)
}

func almostEqual(a, b float32) bool { return math.Abs(float64(a-b)) < 1e-4 }

// TestParticleGPUZeroParticlesRendersWithoutDraw exercises the whole GPU path with a
// container that has never emitted: Render must complete cleanly, and whatever the
// kernel compacts must be nothing (every capacity slot starts dead — see
// particle_update.comp.glsl), so there is nothing for drawParticles to draw.
func TestParticleGPUZeroParticlesRendersWithoutDraw(t *testing.T) {
	r, scene, cam := gpuParticleTestScene(t)
	quad := r.NewPlaneGeometry(1, 1, 1, 1)
	t.Cleanup(quad.Release)
	mat := r.NewBasicParticleMaterial()
	t.Cleanup(mat.Release)
	config := ParticleConfig{Geometry: quad, Material: mat, Spawn: SpawnFuncFor(func(p *Particle) { p.Lifetime = 1 })}
	c := scene.NewParticleContainer(config, 10)
	scene.Add(c)

	r.Render(scene, cam) // must not panic or hang

	if c.data().buffersReady {
		if ind := readIndirect(c); ind.instanceCount != 0 {
			t.Fatalf("instanceCount = %d, want 0: nothing was ever spawned", ind.instanceCount)
		}
	}
}

// TestParticleGPUNewbornMatchesSpawn checks that a newborn's first compacted record,
// as read back after the render that consumes it, holds exactly the Spawn-time
// values — the update kernel's tail range appends pending records without ageing or
// integrating them (see particle_update.comp.glsl).
func TestParticleGPUNewbornMatchesSpawn(t *testing.T) {
	r, scene, cam := gpuParticleTestScene(t)
	quad := r.NewPlaneGeometry(1, 1, 1, 1)
	t.Cleanup(quad.Release)
	mat := r.NewBasicParticleMaterial()
	t.Cleanup(mat.Release)

	wantPos := glm.Vec3f{1, 2, 3}
	wantVel := glm.Vec3f{0, 5, 0}
	config := ParticleConfig{
		Geometry: quad, Material: mat,
		Spawn: SpawnFuncFor(func(p *Particle) {
			p.Position = wantPos
			p.Velocity = wantVel
			p.Lifetime = 10
		}),
	}
	c := scene.NewParticleContainer(config, 4)
	scene.Add(c)
	e := &countEmitter{n: 1}
	c.data().emitters = []ParticleEmitter{e}
	c.Update(1.0 / 60)

	r.Render(scene, cam)

	ind := readIndirect(c)
	if ind.instanceCount != 1 {
		t.Fatalf("instanceCount = %d, want 1", ind.instanceCount)
	}
	got := readParticles(c, ind.instanceCount)[0]
	if got.position != wantPos {
		t.Errorf("position = %v, want %v (spawn-time value, untouched by this frame's kernel)", got.position, wantPos)
	}
	if got.velocity != wantVel {
		t.Errorf("velocity = %v, want %v", got.velocity, wantVel)
	}
	if got.age != 0 {
		t.Errorf("age = %v, want 0", got.age)
	}
}

// TestParticleGPUGravityDragIntegration hand-computes one update step against the
// default kernel's gravity+drag formula (age+=dt; velocity+=gravity*dt;
// velocity*=exp(-drag*dt); position+=velocity*dt — see particle_update.comp.glsl)
// and checks the GPU's result matches.
func TestParticleGPUGravityDragIntegration(t *testing.T) {
	r, scene, cam := gpuParticleTestScene(t)
	quad := r.NewPlaneGeometry(1, 1, 1, 1)
	t.Cleanup(quad.Release)
	mat := r.NewBasicParticleMaterial()
	t.Cleanup(mat.Release)

	startPos := glm.Vec3f{0, 0, 0}
	startVel := glm.Vec3f{1, 0, 0}
	gravity := glm.Vec3f{0, -9.8, 0}
	const drag = 0.5
	config := ParticleConfig{
		Geometry: quad, Material: mat,
		Spawn: SpawnFuncFor(func(p *Particle) {
			p.Position, p.Velocity, p.Lifetime = startPos, startVel, 10
		}),
		Update: ParticleUpdate{Gravity: gravity, Drag: drag},
	}
	c := scene.NewParticleContainer(config, 4)
	scene.Add(c)
	e := &countEmitter{n: 1}
	c.data().emitters = []ParticleEmitter{e}

	// Frame 1: spawn the newborn (appended verbatim, no integration this frame).
	c.Update(1.0 / 60)
	r.Render(scene, cam)

	// Frame 2: no new emission, a fresh dt — this is the step under test.
	c.data().emitters = nil
	const dt = 1.0 / 30
	c.Update(dt)
	r.Render(scene, cam)

	wantVel := startVel.Add(gravity.Scale(dt)).Scale(float32(math.Exp(-drag * dt)))
	wantPos := startPos.Add(wantVel.Scale(dt))

	ind := readIndirect(c)
	if ind.instanceCount != 1 {
		t.Fatalf("instanceCount = %d, want 1", ind.instanceCount)
	}
	got := readParticles(c, ind.instanceCount)[0]
	if !almostEqual(got.age, dt) {
		t.Errorf("age = %v, want %v", got.age, dt)
	}
	for i := 0; i < 3; i++ {
		if !almostEqual(got.velocity[i], wantVel[i]) {
			t.Errorf("velocity[%d] = %v, want %v", i, got.velocity[i], wantVel[i])
		}
		if !almostEqual(got.position[i], wantPos[i]) {
			t.Errorf("position[%d] = %v, want %v", i, got.position[i], wantPos[i])
		}
	}
}

// TestParticleGPUDeathIsCompacted checks that a particle whose age has passed its
// lifetime is dropped by the compaction step, leaving instanceCount at 0.
func TestParticleGPUDeathIsCompacted(t *testing.T) {
	r, scene, cam := gpuParticleTestScene(t)
	quad := r.NewPlaneGeometry(1, 1, 1, 1)
	t.Cleanup(quad.Release)
	mat := r.NewBasicParticleMaterial()
	t.Cleanup(mat.Release)

	config := ParticleConfig{
		Geometry: quad, Material: mat,
		Spawn: SpawnFuncFor(func(p *Particle) { p.Lifetime = 0.01 }),
	}
	c := scene.NewParticleContainer(config, 4)
	scene.Add(c)
	e := &countEmitter{n: 1}
	c.data().emitters = []ParticleEmitter{e}

	c.Update(1.0 / 60)
	r.Render(scene, cam) // spawns it

	c.data().emitters = nil
	c.Update(1.0) // one full second, far past the 0.01s lifetime
	r.Render(scene, cam)

	if ind := readIndirect(c); ind.instanceCount != 0 {
		t.Fatalf("instanceCount = %d, want 0 (particle should have died and been dropped)", ind.instanceCount)
	}
}
