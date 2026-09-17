package materials

import (
	"unsafe"

	"github.com/bluescreen10/pix/colors"
	"github.com/bluescreen10/pix/ref"
	"github.com/bluescreen10/pix/shaders"
	"github.com/bluescreen10/pix/textures"
)

// BasicParticleMaterial is BasicMaterial's particle-path counterpart: the same
// unlit look (base color × color × color map, plus emissive), but rendering
// through pix's dedicated particle shaders instead of the mesh vertex-pull/Drawable
// contract — see particle_common.glsl for why particles need their own contract
// rather than reusing a mesh material's Forward(). This is the only material type
// pix.ParticleConfig.Material accepts today: unlike Material generally, a
// particle-safe shader pair can't be discovered structurally (BlinnPhongMaterial and
// PBRMaterial expose the same Color()/Emissive()/ColorMap() accessor names but write
// unrelated, larger GPU records), so rather than gate that with an interface marker,
// the type itself simply IS the particle shader pair — Vertex/Forward below are
// shaders.ParticleDraw/ParticleBasicForward, not a mesh shader.
type BasicParticleMaterial struct {
	pool *Pool
	ref  ref.Ref

	emissive colors.RGB32F

	color        colors.RGBA32F
	colorMap     textures.Texture
	colorSampler uint32
}

// NewBasicParticleMaterial creates an unlit particle material with an unbound color
// map.
func NewBasicParticleMaterial(store *Store) *BasicParticleMaterial {
	st := store.Pool(Shader{Vertex: shaders.ParticleDraw, Forward: shaders.ParticleBasicForward}, "Basic Particle Material")
	m := &BasicParticleMaterial{color: colors.RGBA32F{1, 1, 1, 1}}
	m.pool = st
	m.ref = st.Create(m)
	return m
}

// Bytes implements Instance: the 48-byte record matching the Material struct in
// shaders/src/particle_basic.frag.glsl — deliberately the same layout
// BasicMaterial.Bytes writes (see that method's doc comment), since this is the
// same unlit look, just rendered through the particle path's own shader pair.
func (m *BasicParticleMaterial) Bytes() []byte {
	rec := struct {
		color        colors.RGBA32F
		emissive     colors.RGB32F
		_            float32
		colorMap     uint32
		colorSampler uint32
		flags        uint32
		_            uint32
	}{
		color:        m.color,
		emissive:     m.emissive,
		colorMap:     MapIndex(m.colorMap),
		colorSampler: m.colorSampler,
		flags:        MapFlag(m.colorMap, MatColorMap),
	}
	return unsafe.Slice((*byte)(unsafe.Pointer(&rec)), unsafe.Sizeof(rec))
}

// Dispose implements Instance: drop every texture this material bound.
func (m *BasicParticleMaterial) Dispose() {
	m.colorMap.Release()
}

func (m *BasicParticleMaterial) dirty() {
	m.pool.MarkDirty(m.ref.ID())
}

func (m *BasicParticleMaterial) Color() colors.RGBA32F {
	return m.color
}

func (m *BasicParticleMaterial) SetColor(color colors.RGBA32F) {
	m.color = color
	m.dirty()
}

// Emissive is the light the particle emits on its own. It has no alpha: emitted
// light is not a coverage, and the record's fourth channel is padding the shader
// never reads.
func (m *BasicParticleMaterial) Emissive() colors.RGB32F {
	return m.emissive
}

func (m *BasicParticleMaterial) SetEmissive(color colors.RGB32F) {
	m.emissive = color
	m.dirty()
}

func (m *BasicParticleMaterial) ColorMap() textures.Texture {
	return m.colorMap
}

// SetColorMap binds the base-color map; pass a zero textures.Texture to clear it.
// The material takes its own reference, so the caller may release theirs.
func (m *BasicParticleMaterial) SetColorMap(texture textures.Texture) {
	old := m.colorMap
	m.colorMap = texture.Copy()
	old.Release()
	m.dirty()
}

func (m *BasicParticleMaterial) ColorMapSampler() uint32 {
	return m.colorSampler
}

func (m *BasicParticleMaterial) SetColorMapSampler(sampler uint32) {
	m.colorSampler = sampler
	m.dirty()
}

// --- Material ---

// Copy returns another handle to the same instance (refcount++); see BasicMaterial's
// Copy doc comment — the same reasoning applies here.
func (m *BasicParticleMaterial) Copy() Material {
	m.ref.Copy()
	return m
}

// Release drops this handle's reference. At refcount 0 the store disposes the slot,
// which releases the textures the material bound.
func (m *BasicParticleMaterial) Release() {
	m.ref.Release()
}

// Valid reports whether the underlying instance is still alive.
func (m *BasicParticleMaterial) IsValid() bool {
	return m.ref.IsValid()
}

// Vertex is the particle vertex-pull shader — not nil, unlike a mesh material: there
// is no default particle vertex stage to fall back to.
func (m *BasicParticleMaterial) Vertex() []byte {
	return m.pool.Shader().Vertex
}

// Forward is the always-present single-pass shader (unlit particle shading).
func (m *BasicParticleMaterial) Forward() []byte {
	return m.pool.Shader().Forward
}

// Deferred is always nil: unlit particles have no surface to hand a deferred
// lighting pass, and always render forward.
func (m *BasicParticleMaterial) Deferred() []byte {
	return m.pool.Shader().Deferred
}

// Lighting is always nil; see Deferred.
func (m *BasicParticleMaterial) Lighting() []byte {
	return m.pool.Shader().Lighting
}

// Cull reports which triangle faces are discarded.
func (m *BasicParticleMaterial) Cull() CullMode {
	return m.pool.Cull(m.ref.ID())
}

// SetCull sets which faces are culled (CullNone = double-sided).
func (m *BasicParticleMaterial) SetCull(mode CullMode) {
	m.pool.SetCull(m.ref.ID(), mode)
}

// SetDoubleSided is a convenience for SetCull(CullNone) / SetCull(CullBack).
func (m *BasicParticleMaterial) SetDoubleSided(enabled bool) {
	if enabled {
		m.SetCull(CullNone)
	} else {
		m.SetCull(CullBack)
	}
}

// Blend reports the material's blend mode.
func (m *BasicParticleMaterial) Blend() BlendMode {
	return m.pool.Blend(m.ref.ID())
}

// SetBlend sets the material's blend mode (Opaque/Alpha/Additive) — most particle
// effects (smoke, fire, sparks) want BlendAlpha or BlendAdditive; the default
// constructed value is BlendOpaque, matching every other material type's default.
func (m *BasicParticleMaterial) SetBlend(mode BlendMode) {
	m.pool.SetBlend(m.ref.ID(), mode)
}

// Pool returns the pool this material's records live in.
func (m *BasicParticleMaterial) Pool() *Pool {
	return m.pool
}

// Hash is this material type's pipeline identity: its pool's, since these materials
// vary only by shader. Distinct from BasicMaterial's Hash — a different Forward
// shader (ParticleBasicForward vs BasicForward) means a different pool.
func (m *BasicParticleMaterial) Hash() uint32 {
	return m.pool.Hash()
}

// ID is the instance's index within its store; RecordsAddr is the store's record
// buffer address, resolved at draw time because it moves when the store grows.
func (m *BasicParticleMaterial) ID() uint32 {
	return m.ref.ID()
}

func (m *BasicParticleMaterial) RecordsAddr() uint64 {
	return m.pool.RecordsAddr()
}
