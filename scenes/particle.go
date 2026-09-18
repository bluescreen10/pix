package scenes

import (
	"github.com/bluescreen10/pix/colors"
	"github.com/bluescreen10/pix/geometries"
	"github.com/bluescreen10/pix/glm"
	"github.com/bluescreen10/pix/materials"
	"github.com/chewxy/math32"
)

// Particle is a CPU-visible record only at the moment a Spawner initializes it (see
// ParticleSpawner). Once appended, the same fields live in a GPU-resident particle
// record at an identical layout (see ParticleRecord below); nothing after Spawn
// reaches back into Go to read or write a living particle.
type Particle struct {
	Position glm.Vec3f
	Velocity glm.Vec3f
	Rotation glm.Quat[float32]
	Scale    glm.Vec3f
	Color    colors.RGBA32F
	Data     glm.Vec4f

	Age      float32
	Lifetime float32

	InitialScale glm.Vec3f
	InitialColor colors.RGBA32F
}

// defaultParticle is the value every Particle starts from before Spawn runs.
var defaultParticle = Particle{
	Rotation: glm.Quat[float32]{0, 0, 0, 1},
	Scale:    glm.Vec3f{1, 1, 1},
	Color:    colors.RGBA32F{1, 1, 1, 1},
	Lifetime: 1,
}

// ParticleRecord is the GPU-resident mirror of Particle: scalar layout, pointers
// first where there are any (there are none here — every field is inline), matching
// the buffer_reference convention every other root/record struct in this codebase
// uses (see drawable.go). Pinning this layout is what makes a custom update shader
// (see ParticleUpdate) possible at all: anything that reads or writes a particle
// record depends on this exact field order and size.
type ParticleRecord struct {
	Position glm.Vec3f
	Velocity glm.Vec3f
	Rotation glm.Quat[float32]
	Scale    glm.Vec3f
	Color    colors.RGBA32F
	Data     glm.Vec4f

	Age      float32
	Lifetime float32

	InitialScale glm.Vec3f
	InitialColor colors.RGBA32F
}

func toParticleRecord(p *Particle) ParticleRecord {
	return ParticleRecord{
		Position:     p.Position,
		Velocity:     p.Velocity,
		Rotation:     p.Rotation,
		Scale:        p.Scale,
		Color:        p.Color,
		Data:         p.Data,
		Age:          p.Age,
		Lifetime:     p.Lifetime,
		InitialScale: p.InitialScale,
		InitialColor: p.InitialColor,
	}
}

// ParticleSpawner initializes an individual particle at birth. See the package-level
// particles helper for a function-adapter implementation (SpawnFunc).
type ParticleSpawner interface {
	Spawn(p *Particle)
}

// ParticleEmitter requests a number of births for a simulation step. Emit only
// schedules births — it does not receive or modify particles; the container's single
// Spawner initializes every birth, from every emitter. See the particles package for
// built-in Rate and Burst implementations.
type ParticleEmitter interface {
	Emit(dt float32) int
	Reset()
}

// ParticleCurve is a linear ramp evaluated at a particle's clamped Age/Lifetime,
// multiplying its birth value. A disabled curve leaves the field at its birth value
// for the particle's whole life — see ParticleUpdate.
type ParticleCurve struct {
	Enabled    bool
	Start, End float32
}

// ParticleUpdate selects and parameterizes the per-frame GPU kernel that ages,
// integrates, and evaluates behavior for every living particle. A nil Shader selects
// Pix's default kernel, configured by the fields below.
//
// A non-nil Shader is not yet implemented: constructing a container with one panics.
// The ABI it will need to honor is fixed by ParticleRecord above (the shader reads
// and writes that exact layout), but the compaction-wrapping machinery the contract
// requires is not built yet — see docs/particle-system.md.
type ParticleUpdate struct {
	Shader []byte

	Gravity         glm.Vec3f
	Drag            float32
	SizeOverLife    ParticleCurve
	OpacityOverLife ParticleCurve

	// Params is an opaque per-container blob for a custom Shader's own tunables.
	// Pix does not interpret it. Unused until custom shaders are implemented.
	Params []byte
}

// ParticleFacing controls how a particle's quad or mesh is oriented for rendering.
type ParticleFacing uint8

const (
	// ParticleFaceParticle uses particle rotation and scale followed by the
	// container's world transform, like an instanced mesh. The only facing mode
	// implemented in this version.
	ParticleFaceParticle ParticleFacing = iota
	// ParticleFaceCamera billboards toward each rendering camera. Not yet
	// implemented — constructing a container with it panics. See
	// docs/particle-system.md's billboard conventions, which need review before
	// implementation.
	ParticleFaceCamera
)

// ParticleSort controls per-frame draw ordering.
type ParticleSort uint8

const (
	// ParticleSortNone uses GPU compaction order. The only sort mode implemented
	// in this version.
	ParticleSortNone ParticleSort = iota
	// ParticleSortBackToFront sorts particle centers by camera-space depth per
	// view. Not yet implemented — constructing a container with it panics.
	ParticleSortBackToFront
)

// ParticleConfig configures a new ParticleContainer. Geometry, Facing, Sort, Spawn,
// the emitter list, and the update kernel are fixed at construction in this version.
type ParticleConfig struct {
	Geometry geometries.Geometry
	// Material is a *materials.BasicParticleMaterial — its own dedicated material
	// type, carrying pix's particle shaders (shaders.ParticleDraw/ParticleBasicForward)
	// directly rather than a mesh material's Forward(). Concrete rather than an
	// interface: a particle-safe shader pair can't be discovered structurally
	// (BlinnPhongMaterial/PBRMaterial expose the same Color()/Emissive()/ColorMap()
	// accessor names but write unrelated GPU records), so the type itself is the
	// guarantee.
	Material *materials.BasicParticleMaterial
	Facing   ParticleFacing
	Sort     ParticleSort
	Emitters []ParticleEmitter
	Spawn    ParticleSpawner
	Update   ParticleUpdate
}

// particleData is the per-container payload stored in Scene.particleContainers,
// mirroring meshData's shape and ownership rules (mesh.go).
type particleData struct {
	geometry geometries.Geometry
	material materials.Material
	facing   ParticleFacing
	sort     ParticleSort
	update   ParticleUpdate

	emitters []ParticleEmitter
	spawn    ParticleSpawner

	capacity uint32
	// alive is this container's live particle count as of the last completed
	// Update — see ParticleContainer.Update. It is a CPU mirror kept in sync by
	// staging, not read back from the GPU compaction counter.
	alive uint32

	// pending holds particle records this step's Spawn calls produced; drained and
	// uploaded, alongside dt, by the renderer during the next Render (see
	// dispatchParticleUpdate in renderer.go). emitting is false after StopEmission.
	pending  []ParticleRecord
	dt       float32
	dtStaged bool
	emitting bool

	// id is this system's stable identity. The renderer's simulation buffers are keyed
	// on it, and those buffers are the simulation state, so it is never reused.
	id ParticleID
	// epoch increments on Clear, telling the renderer to start this system's buffers
	// empty rather than keeping whatever was compacted into them last frame.
	epoch     uint64
	ownerNode uint32
}

// ParticleContainer is a typed node handle for a GPU-simulated particle system. It
// embeds Node, so all hierarchy and transform methods are available directly. See
// docs/particle-system.md for the full design.
type ParticleContainer struct{ Node }

func (c ParticleContainer) data() *particleData {
	return &c.scene.particleContainers[c.scene.payload[c.slot()]]
}

// NewParticleContainer creates a particle container from geometry + material (both
// renderer-owned; the scene takes its own references, Copy, so the caller may
// Release theirs) and a fixed maximum number of live particles. A new container is
// empty, with emission enabled. Zero capacity is valid; negative capacity is a
// programming error.
//
// ParticleFaceCamera, ParticleSortBackToFront, and a non-nil Update.Shader are not
// yet implemented in this version and panic here rather than silently degrading.
//
// config.Material is a *materials.BasicParticleMaterial (see ParticleConfig's doc
// comment) — its own dedicated type carrying pix's particle shaders directly,
// enforced at compile time by the field's type rather than a runtime check.
func (s *Scene) NewParticleContainer(config ParticleConfig, capacity int) ParticleContainer {
	if capacity < 0 {
		panic("pix: particle capacity must not be negative")
	}
	if config.Facing == ParticleFaceCamera {
		panic("pix: ParticleFaceCamera is not yet implemented")
	}
	if config.Sort == ParticleSortBackToFront {
		panic("pix: ParticleSortBackToFront is not yet implemented")
	}
	if config.Update.Shader != nil {
		panic("pix: custom particle update shaders are not yet implemented")
	}

	emitters := make([]ParticleEmitter, len(config.Emitters))
	for i, e := range config.Emitters {
		if e == nil {
			panic("pix: nil entry in ParticleConfig.Emitters")
		}
		emitters[i] = e
	}

	id := s.allocNode(KindParticleContainer)
	payloadIdx := uint32(len(s.particleContainers))
	s.particleContainers = append(s.particleContainers, particleData{
		geometry:  config.Geometry.Copy(),
		material:  config.Material.Copy(),
		facing:    config.Facing,
		sort:      config.Sort,
		update:    config.Update,
		emitters:  emitters,
		spawn:     config.Spawn,
		capacity:  uint32(capacity),
		emitting:  true,
		ownerNode: id.index,
		id:        s.newParticleID(),
	})
	s.payload[id.index] = payloadIdx
	return ParticleContainer{Node{scene: s, id: id}}
}

// Capacity returns the container's maximum number of live particles.
func (c ParticleContainer) Capacity() int { return int(c.data().capacity) }

// StartEmission resumes emitter clocks without resetting them.
func (c ParticleContainer) StartEmission() { c.data().emitting = true }

// StopEmission freezes emitter clocks while living particles continue updating.
func (c ParticleContainer) StopEmission() { c.data().emitting = false }

// Geometry returns the container's geometry handle.
func (c ParticleContainer) Geometry() geometries.Geometry { return c.data().geometry }

// Material returns the container's material handle.
func (c ParticleContainer) Material() materials.Material { return c.data().material }

// Update stages this step's dt and runs emission and spawning. It does not itself
// dispatch GPU work or wait for GPU completion: like culling and skinning, the
// update kernel is recorded by the renderer during Render's encode phase, from the
// state staged here (see dispatchParticleUpdate in renderer.go). Calling Update more
// than once before the next Render stages the latest call's dt and accumulates
// emission from every call; it does not run the GPU kernel more than once.
//
// Update(0) is a no-op, including emission. Negative or nonfinite dt is a
// programming error.
func (c ParticleContainer) Update(dt float32) {
	if dt == 0 {
		return
	}
	if dt < 0 || math32.IsNaN(dt) || math32.IsInf(dt, 0) {
		panic("pix: ParticleContainer.Update requires a finite, nonnegative dt")
	}
	d := c.data()
	d.dt += dt
	d.dtStaged = true

	if !d.emitting {
		return
	}
	// Available slots account for this step's not-yet-uploaded pending births too,
	// so calling Update several times before the next Render still respects capacity
	// rather than staging more newborns than the container can ever hold.
	avail := int(d.capacity) - int(d.alive) - len(d.pending)
	for _, e := range d.emitters {
		n := e.Emit(dt)
		if n < 0 {
			panic("pix: ParticleEmitter.Emit returned a negative count")
		}
		for i := 0; i < n && avail > 0; i++ {
			p := defaultParticle
			d.spawn.Spawn(&p)
			if p.Lifetime <= 0 {
				continue // rejected birth: does not consume a slot, but did consume this attempt
			}
			p.Age = 0
			p.InitialScale = p.Scale
			p.InitialColor = p.Color
			d.pending = append(d.pending, toParticleRecord(&p))
			avail--
		}
	}
}

// Clear removes living particles without changing emission state or clocks.
func (c ParticleContainer) Clear() {
	d := c.data()
	d.alive = 0
	d.pending = d.pending[:0]
	d.epoch++ // the renderer zeroes its buffers when it sees this change
}

// Reset clears particles, calls each emitter's Reset, and enables emission. It does
// not reset arbitrary RNG state captured by user callbacks or promise deterministic
// replay.
func (c ParticleContainer) Reset() {
	c.Clear()
	d := c.data()
	d.emitting = true
	for _, e := range d.emitters {
		e.Reset()
	}
}

func (s *Scene) swapRemoveParticles(payloadIdx uint32) {
	d := &s.particleContainers[payloadIdx]
	d.geometry.Release()
	d.material.Release()
	// The simulation buffers are the renderer's; it retires them when this system stops
	// appearing in the packet (see renderState.retireUnseenParticles).
	last := uint32(len(s.particleContainers) - 1)
	if payloadIdx != last {
		s.particleContainers[payloadIdx] = s.particleContainers[last]
		s.payload[s.particleContainers[payloadIdx].ownerNode] = payloadIdx
	}
	s.particleContainers = s.particleContainers[:last]
}

// SystemID is this container's stable particle-system identity — what a renderer keys
// its simulation buffers on. Distinct from Node.ID, which identifies the graph node.
func (c ParticleContainer) SystemID() ParticleID { return c.data().id }

// SetEmitters replaces the emitters driving this container. Emission is otherwise
// configured at construction; this exists because emitters are the one part of a
// container a caller reasonably swaps at runtime — turning a burst into a steady rate,
// or silencing a system without tearing down its simulation state.
func (c ParticleContainer) SetEmitters(e []ParticleEmitter) {
	for _, em := range e {
		if em == nil {
			panic("pix: ParticleContainer.SetEmitters: nil emitter")
		}
	}
	c.data().emitters = e
}

// Pending is the births queued by Update calls since the last frame that rendered this
// container, in spawn order. The slice is the container's own storage — read it, do not
// retain or modify it — and it empties when a rendered frame consumes the step.
func (c ParticleContainer) Pending() []ParticleRecord { return c.data().pending }

// Alive is the container's live particle count as of the last consumed step. It is a
// CPU estimate advanced by births and never read back down as particles die on the GPU,
// so it over-counts until a Clear; see the ParticleContainer docs.
func (c ParticleContainer) Alive() uint32 { return c.data().alive }

// Emitting reports whether emission is enabled (see StopEmission/StartEmission).
func (c ParticleContainer) Emitting() bool { return c.data().emitting }

// PendingStep is the simulation time accumulated by Update since the last rendered
// frame — what the next frame will advance the simulation by.
func (c ParticleContainer) PendingStep() float32 { return c.data().dt }
