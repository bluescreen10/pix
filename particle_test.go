package pix_test

import (
	"testing"

	"github.com/bluescreen10/pix"
	"github.com/bluescreen10/pix/colors"
	"github.com/bluescreen10/pix/geometries"
	"github.com/bluescreen10/pix/glm"
	"github.com/bluescreen10/pix/materials"
	"github.com/bluescreen10/pix/scenes"
)

// countEmitter emits n particles on every positive Emit call and records how many
// times it was reset, for tests that need to observe emission independent of the
// particles package's Rate/Burst timing.
type countEmitter struct {
	n       int
	resets  int
	emitted int
}

func (e *countEmitter) Emit(dt float32) int {
	if dt <= 0 {
		return 0
	}
	e.emitted++
	return e.n
}
func (e *countEmitter) Reset() {
	e.resets++
}

func newParticleTestScene(t *testing.T) (*pix.Renderer, *scenes.Scene, scenes.ParticleConfig) {
	t.Helper()
	r, err := pix.NewOffscreenRenderer(16, 16)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(r.Destroy)
	scene := scenes.New()
	t.Cleanup(scene.Destroy)

	quad := r.NewPlaneGeometry(1, 1, 1, 1)
	mat := r.NewBasicParticleMaterial()
	config := scenes.ParticleConfig{
		Geometry: quad,
		Material: mat,
		Spawn: SpawnFuncFor(func(p *scenes.Particle) {
			p.Lifetime = 1
		}),
	}
	return r, scene, config
}

// SpawnFuncFor lets this file's tests supply a spawn closure without importing the
// particles package (which itself imports pix, and pix's own test package cannot
// import a package that imports it back).
type spawnFunc func(*scenes.Particle)

func (f spawnFunc) Spawn(p *scenes.Particle) {
	f(p)
}

func SpawnFuncFor(f func(*scenes.Particle)) scenes.ParticleSpawner {
	return spawnFunc(f)
}

func TestParticleContainerDefaults(t *testing.T) {
	_, scene, config := newParticleTestScene(t)
	c := scene.NewParticleContainer(config, 100)
	if got := c.Capacity(); got != 100 {
		t.Fatalf("scenes.Capacity() = %d, want 100", got)
	}
}

func TestParticleContainerZeroCapacity(t *testing.T) {
	_, scene, config := newParticleTestScene(t)
	c := scene.NewParticleContainer(config, 0)
	if got := c.Capacity(); got != 0 {
		t.Fatalf("scenes.Capacity() = %d, want 0", got)
	}
}

func TestParticleContainerNegativeCapacityPanics(t *testing.T) {
	_, scene, config := newParticleTestScene(t)
	defer func() {
		if recover() == nil {
			t.Fatal("expected panic for negative capacity")
		}
	}()
	scene.NewParticleContainer(config, -1)
}

func TestParticleFaceCameraPanics(t *testing.T) {
	_, scene, config := newParticleTestScene(t)
	config.Facing = scenes.ParticleFaceCamera
	defer func() {
		if recover() == nil {
			t.Fatal("expected panic for scenes.ParticleFaceCamera")
		}
	}()
	scene.NewParticleContainer(config, 10)
}

func TestParticleCustomShaderPanics(t *testing.T) {
	_, scene, config := newParticleTestScene(t)
	config.Update.Shader = []byte("kernel void main0() {}")
	defer func() {
		if recover() == nil {
			t.Fatal("expected panic for a non-nil scenes.Update.Shader")
		}
	}()
	scene.NewParticleContainer(config, 10)
}

func TestParticleNilEmitterEntryPanics(t *testing.T) {
	_, scene, config := newParticleTestScene(t)
	config.Emitters = []scenes.ParticleEmitter{nil}
	defer func() {
		if recover() == nil {
			t.Fatal("expected panic for a nil emitter entry")
		}
	}()
	scene.NewParticleContainer(config, 10)
}

func TestParticleUpdateZeroIsNoop(t *testing.T) {
	_, scene, config := newParticleTestScene(t)
	e := &countEmitter{n: 5}
	config.Emitters = []scenes.ParticleEmitter{e}
	c := scene.NewParticleContainer(config, 100)
	c.Update(0)
	if e.emitted != 0 {
		t.Fatalf("scenes.Update(0) called Emit %d times, want 0", e.emitted)
	}
	if got := c.Pending(); len(got) != 0 {
		t.Fatalf("scenes.Update(0) staged %d pending particles, want 0", len(got))
	}
}

func TestParticleUpdateNegativeDtPanics(t *testing.T) {
	_, scene, config := newParticleTestScene(t)
	c := scene.NewParticleContainer(config, 10)
	defer func() {
		if recover() == nil {
			t.Fatal("expected panic for negative dt")
		}
	}()
	c.Update(-1)
}

func TestParticleEmissionSpawnsUpToCapacity(t *testing.T) {
	_, scene, config := newParticleTestScene(t)
	e := &countEmitter{n: 1000}
	config.Emitters = []scenes.ParticleEmitter{e}
	c := scene.NewParticleContainer(config, 7)
	c.Update(1.0 / 60)
	got := len(c.Pending())
	if got != 7 {
		t.Fatalf("pending = %d, want 7 (capped at capacity)", got)
	}
}

func TestParticleOverflowDoesNotQueue(t *testing.T) {
	_, scene, config := newParticleTestScene(t)
	e := &countEmitter{n: 5}
	config.Emitters = []scenes.ParticleEmitter{e}
	c := scene.NewParticleContainer(config, 3)
	c.Update(1.0 / 60) // requests 5, only 3 slots: 3 staged, the other 2 dropped
	if got := len(c.Pending()); got != 3 {
		t.Fatalf("pending after first scenes.Update = %d, want 3", got)
	}
	c.Update(1.0 / 60) // still full (pending counts against capacity): nothing more staged
	if got := len(c.Pending()); got != 3 {
		t.Fatalf("pending after second scenes.Update = %d, want 3 (overflow must not queue)", got)
	}
	if e.emitted != 2 {
		t.Fatalf("emitter was called %d times, want 2 (clocks advance even while full)", e.emitted)
	}
}

func TestParticleRejectedBirthDoesNotConsumeSlot(t *testing.T) {
	_, scene, config := newParticleTestScene(t)
	calls := 0
	config.Spawn = SpawnFuncFor(func(p *scenes.Particle) {
		calls++
		if calls%2 == 0 {
			p.Lifetime = -1 // reject every other birth
		} else {
			p.Lifetime = 1
		}
	})
	e := &countEmitter{n: 4}
	config.Emitters = []scenes.ParticleEmitter{e}
	c := scene.NewParticleContainer(config, 10)
	c.Update(1.0 / 60)
	if got := len(c.Pending()); got != 2 {
		t.Fatalf("pending = %d, want 2 (half the 4 attempts rejected)", got)
	}
}

func TestParticleSpawnDefaults(t *testing.T) {
	_, scene, config := newParticleTestScene(t)
	var got scenes.Particle
	config.Spawn = SpawnFuncFor(func(p *scenes.Particle) {
		got = *p // defaults, before this closure's own edits
		p.Lifetime = 1
	})
	e := &countEmitter{n: 1}
	config.Emitters = []scenes.ParticleEmitter{e}
	c := scene.NewParticleContainer(config, 10)
	c.Update(1.0 / 60)

	if got.Position != (glm.Vec3f{}) {
		t.Errorf("default scenes.Position = %v, want zero", got.Position)
	}
	if got.Rotation != (glm.Quat[float32]{0, 0, 0, 1}) {
		t.Errorf("default scenes.Rotation = %v, want identity", got.Rotation)
	}
	if got.Scale != (glm.Vec3f{1, 1, 1}) {
		t.Errorf("default scenes.Scale = %v, want (1,1,1)", got.Scale)
	}
	if got.Color[3] != 1 {
		t.Errorf("default scenes.Color alpha = %v, want 1", got.Color[3])
	}
	if got.Lifetime != 1 {
		t.Errorf("default scenes.Lifetime = %v, want 1 (spawner's own default)", got.Lifetime)
	}
}

func TestParticleInitialValuesCapturedAfterSpawn(t *testing.T) {
	_, scene, config := newParticleTestScene(t)
	config.Spawn = SpawnFuncFor(func(p *scenes.Particle) {
		p.Scale = glm.Vec3f{2, 3, 4}
		p.Color = [4]float32{0.5, 0.25, 0.1, 0.75}
		p.Lifetime = 1
	})
	e := &countEmitter{n: 1}
	config.Emitters = []scenes.ParticleEmitter{e}
	c := scene.NewParticleContainer(config, 10)
	c.Update(1.0 / 60)

	pending := c.Pending()
	if len(pending) != 1 {
		t.Fatalf("pending = %d, want 1", len(pending))
	}
	rec := pending[0]
	if rec.InitialScale != (glm.Vec3f{2, 3, 4}) {
		t.Errorf("initialScale = %v, want the spawner's scenes.Scale", rec.InitialScale)
	}
	if rec.InitialColor != [4]float32{0.5, 0.25, 0.1, 0.75} {
		t.Errorf("initialColor = %v, want the spawner's scenes.Color", rec.InitialColor)
	}
	if rec.Age != 0 {
		t.Errorf("age = %v, want 0 at birth", rec.Age)
	}
}

func TestParticleStopStartEmission(t *testing.T) {
	_, scene, config := newParticleTestScene(t)
	e := &countEmitter{n: 1}
	config.Emitters = []scenes.ParticleEmitter{e}
	c := scene.NewParticleContainer(config, 10)

	c.StopEmission()
	c.Update(1.0 / 60)
	if e.emitted != 0 {
		t.Fatalf("emitter ran %d times while stopped, want 0", e.emitted)
	}
	if got := len(c.Pending()); got != 0 {
		t.Fatalf("pending while stopped = %d, want 0", got)
	}

	c.StartEmission()
	c.Update(1.0 / 60)
	if got := len(c.Pending()); got != 1 {
		t.Fatalf("pending after resuming = %d, want 1", got)
	}
}

func TestParticleClear(t *testing.T) {
	_, scene, config := newParticleTestScene(t)
	e := &countEmitter{n: 3}
	config.Emitters = []scenes.ParticleEmitter{e}
	c := scene.NewParticleContainer(config, 10)
	c.Update(1.0 / 60)
	if got := len(c.Pending()); got != 3 {
		t.Fatalf("pending before Clear = %d, want 3", got)
	}
	c.Clear()
	if c.Alive() != 0 || len(c.Pending()) != 0 {
		t.Fatalf("after Clear: alive=%d pending=%d, want 0, 0", c.Alive(), len(c.Pending()))
	}
}

func TestParticleReset(t *testing.T) {
	_, scene, config := newParticleTestScene(t)
	e := &countEmitter{n: 3}
	config.Emitters = []scenes.ParticleEmitter{e}
	c := scene.NewParticleContainer(config, 10)
	c.Update(1.0 / 60)
	c.StopEmission()
	c.Reset()

	if len(c.Pending()) != 0 {
		t.Fatalf("pending after Reset = %d, want 0", len(c.Pending()))
	}
	if !c.Emitting() {
		t.Fatal("Reset must re-enable emission")
	}
	if e.resets != 1 {
		t.Fatalf("emitter Reset called %d times, want 1", e.resets)
	}
}

func TestParticleFreshEmitterInstancesIsolateTiming(t *testing.T) {
	// particles.Rate is exercised directly in the particles package's own tests;
	// this checks the container-level contract that two containers never share
	// emitter state even when constructed from the same ParticleConfig value.
	_, scene, config := newParticleTestScene(t)
	e1, e2 := &countEmitter{n: 1}, &countEmitter{n: 1}
	config.Emitters = []scenes.ParticleEmitter{e1}
	c1 := scene.NewParticleContainer(config, 10)
	config.Emitters = []scenes.ParticleEmitter{e2}
	c2 := scene.NewParticleContainer(config, 10)

	c1.Update(1.0 / 60)
	if e1.emitted != 1 || e2.emitted != 0 {
		t.Fatalf("e1.emitted=%d e2.emitted=%d, want 1, 0 (isolated)", e1.emitted, e2.emitted)
	}
	if got := len(c1.Pending()); got != 1 {
		t.Fatalf("c1 pending = %d, want 1", got)
	}
	if got := len(c2.Pending()); got != 0 {
		t.Fatalf("c2 pending = %d, want 0 (c1's scenes.Update must not affect c2)", got)
	}
}

// TestParticlesDrawBackToFront renders two 50%-alpha particles of one container,
// overlapping over black — a blue one spawned first, farther along -z, and a red one —
// from in front and then from behind. A back-to-front container draws the nearer one
// last, from either side: the overlap is half the nearer colour plus a quarter of the
// farther, which the display target encodes as 188 and 137. The container's own order,
// the order they were spawned in, can only be right from one side.
func TestParticlesDrawBackToFront(t *testing.T) {
	r, err := pix.NewOffscreenRenderer(postSize, postSize)
	if err != nil {
		t.Fatal(err)
	}
	defer r.Destroy()
	r.SetClearColor(colors.RGBA32F{0, 0, 0, 1})
	scene := scenes.New()
	defer scene.Destroy()

	quad := r.GeometryStore.Create(geometries.GeometryConfig{
		Attributes: []geometries.Attribute{
			geometries.NewAttribute(geometries.AttributePosition, geometries.Float32x3, []glm.Vec3f{{-0.8, -0.8, 0}, {0.8, -0.8, 0}, {0.8, 0.8, 0}, {-0.8, 0.8, 0}}),
		},
		Indices: []uint32{0, 1, 2, 0, 2, 3},
	})
	material := r.NewBasicParticleMaterial()
	material.SetBlend(materials.BlendAlpha)
	material.SetDoubleSided(true)
	spawned := 0
	container := scene.NewParticleContainer(scenes.ParticleConfig{
		Geometry: quad,
		Material: material,
		Sort:     scenes.ParticleSortBackToFront,
		Emitters: []scenes.ParticleEmitter{&countEmitter{n: 2}},
		Spawn: SpawnFuncFor(func(p *scenes.Particle) {
			p.Lifetime = 100
			if spawned == 0 {
				p.Position, p.Color = glm.Vec3f{0, 0, -0.5}, colors.RGBA32F{0, 0, 1, 0.5}
			} else {
				p.Position, p.Color = glm.Vec3f{0, 0, 0}, colors.RGBA32F{1, 0, 0, 0.5}
			}
			spawned++
		}),
	}, 2)
	scene.Add(container)
	container.Update(1.0 / 60)

	cam := scene.NewPerspectiveCamera(45, 1, 0.1, 100)
	scene.Add(cam)
	for _, view := range []struct {
		z    float32
		want [3]byte
		name string
	}{
		{2, [3]byte{188, 0, 137}, "from in front, red is nearer"},
		{-2.5, [3]byte{137, 0, 188}, "from behind, blue is nearer"},
	} {
		cam.SetPosition(glm.Vec3f{0, 0, view.z})
		cam.LookAt(glm.Vec3f{0, 0, -0.25})
		r.Render(scene)

		if got := pixelAt(r, postSize/2, postSize/2); got != view.want {
			t.Errorf("%s: center = %v, want %v", view.name, got, view.want)
		}
	}
}

// TestParticleSortOrdersEveryPair lays 40 particles of one back-to-front container in a
// row, each overlapping only its neighbours, at depths shuffled against the order they
// were spawned in, and in alternating colours: red, blue, red. They are opaque, so in
// each overlap the one drawn last is the one that shows, and back to front that is
// the nearer. Every adjacent pair is checked, so a sort that misplaces any particle
// shows it; and the container has room for 100, so the sort also has to move the 60
// empty slots out of the way.
func TestParticleSortOrdersEveryPair(t *testing.T) {
	const count, width, height = 40, 400, 20
	const pixelsPerUnit = width / count
	r, err := pix.NewOffscreenRenderer(width, height)
	if err != nil {
		t.Fatal(err)
	}
	defer r.Destroy()
	r.SetClearColor(colors.RGBA32F{0, 0, 0, 1})
	scene := scenes.New()
	defer scene.Destroy()

	// Each particle spans 1.5 units and its neighbours sit 1 unit away, so the half
	// unit between two centres is covered by those two and no other.
	quad := r.GeometryStore.Create(geometries.GeometryConfig{
		Attributes: []geometries.Attribute{
			geometries.NewAttribute(geometries.AttributePosition, geometries.Float32x3, []glm.Vec3f{{-0.75, -0.9, 0}, {0.75, -0.9, 0}, {0.75, 0.9, 0}, {-0.75, 0.9, 0}}),
		},
		Indices: []uint32{0, 1, 2, 0, 2, 3},
	})
	material := r.NewBasicParticleMaterial()
	material.SetBlend(materials.BlendAlpha)
	red, blue := colors.RGBA32F{1, 0, 0, 1}, colors.RGBA32F{0, 0, 1, 1}
	// depth is particle i's z: a fixed shuffle of 40 distinct depths (17 is coprime
	// with 40), so nearness has nothing to do with spawn order.
	depth := func(i int) float32 {
		return -float32(i*17%count) / count
	}
	spawned := 0
	container := scene.NewParticleContainer(scenes.ParticleConfig{
		Geometry: quad,
		Material: material,
		Sort:     scenes.ParticleSortBackToFront,
		Emitters: []scenes.ParticleEmitter{&countEmitter{n: count}},
		Spawn: SpawnFuncFor(func(p *scenes.Particle) {
			p.Lifetime = 100
			p.Position = glm.Vec3f{float32(spawned) - (count-1)/2.0, 0, depth(spawned)}
			p.Color = red
			if spawned%2 == 1 {
				p.Color = blue
			}
			spawned++
		}),
	}, 100)
	scene.Add(container)
	container.Update(1.0 / 60)

	cam := scene.NewOrthographicCamera(-count/2, count/2, -1, 1, 0.1, 100)
	scene.Add(cam)
	cam.SetPosition(glm.Vec3f{0, 0, 5})
	cam.LookAt(glm.Vec3f{0, 0, 0})
	r.Render(scene)

	px := r.Pixels()
	for i := range count - 1 {
		// Halfway between particle i's centre and particle i+1's.
		x := (i + 1) * pixelsPerUnit
		o := (height/2*width + x) * 4
		got := [3]byte{px[o], px[o+1], px[o+2]}
		nearer := i
		if depth(i+1) > depth(i) {
			nearer = i + 1
		}
		want := [3]byte{255, 0, 0}
		if nearer%2 == 1 {
			want = [3]byte{0, 0, 255}
		}
		if got != want {
			t.Errorf("overlap of particles %d and %d = %v, want %v, particle %d's colour, the nearer", i, i+1, got, want, nearer)
		}
	}
}
