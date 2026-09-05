package materials

import (
	"unsafe"

	"github.com/bluescreen10/pix/colors"
	"github.com/bluescreen10/pix/internal/ref"
	"github.com/bluescreen10/pix/shaders"
	"github.com/bluescreen10/pix/textures"
)

// BasicMaterial is an unlit material: base color × vertex color × color map (+
// emissive), ignoring lights.
//
// The material is its own GPU record: these fields are the source of truth and Bytes
// serializes them on demand. Every setter must call dirty(), or the change is never
// uploaded.
type BasicMaterial struct {
	pool *Pool
	ref  ref.Ref

	emissive colors.RGB32F

	// Bound maps. The material holds the reference that keeps each texture alive; the
	// record's bindless index and presence flag are derived from it in Bytes, so they
	// cannot fall out of step with what is actually bound.
	color        colors.RGBA32F
	colorMap     textures.Texture
	colorSampler uint32
}

// NewBasicMaterial creates an unlit material with an unbound color map.
func NewBasicMaterial(store *Store) *BasicMaterial {
	// Forward-only: unlit shading has no surface to hand a deferred lighting pass, so
	// it supplies neither Deferred nor Lighting and always renders through Forward().
	st := store.Pool(Shader{Forward: shaders.BasicForward}, "Basic Material")
	m := &BasicMaterial{color: colors.RGBA32F{1, 1, 1, 1}}
	m.pool = st
	m.ref = st.Create(m)
	return m
}

// Bytes implements Instance: the 48-byte record matching the Material struct
// in shaders/src/scene_basic.frag.glsl. Field order and padding here ARE the GPU
// layout — changing either without changing the shader silently misreads every
// material.
func (m *BasicMaterial) Bytes() []byte {
	rec := struct {
		color        colors.RGBA32F
		emissive     colors.RGB32F
		_            float32 // the shader declares vec4; the 4th channel is unused
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
func (m *BasicMaterial) Dispose() {
	m.colorMap.Release()
}

// dirty marks the record for re-upload in the next Sync. Every setter must call it;
// one that does not leaves the GPU rendering the previous value indefinitely.
func (m *BasicMaterial) dirty() {
	m.pool.MarkDirty(m.ref.ID())
}

func (m *BasicMaterial) Color() colors.RGBA32F {
	return m.color
}

func (m *BasicMaterial) SetColor(color colors.RGBA32F) {
	m.color = color
	m.dirty()
}

// Emissive is the light the surface emits on its own, added after lighting. It has no
// alpha: emitted light is not a coverage, and the record's fourth channel is padding
// the shader never reads.
func (m *BasicMaterial) Emissive() colors.RGB32F {
	return m.emissive
}

func (m *BasicMaterial) SetEmissive(color colors.RGB32F) {
	m.emissive = color
	m.dirty()
}

// The bound textures (or a zero handle).
func (m *BasicMaterial) ColorMap() textures.Texture {
	return m.colorMap
}

// SetColorMap binds the base-color map; pass a zero textures.Texture to clear it. The material
// takes its own reference, so the caller may release theirs.
func (m *BasicMaterial) SetColorMap(texture textures.Texture) {
	old := m.colorMap
	m.colorMap = texture.Copy()
	old.Release() // after the copy, so rebinding a texture to itself cannot free it
	m.dirty()
}

// Per-map sampler getters/setters (bindless sampler heap indices).
func (m *BasicMaterial) ColorMapSampler() uint32 {
	return m.colorSampler
}

func (m *BasicMaterial) SetColorMapSampler(sampler uint32) {
	m.colorSampler = sampler
	m.dirty()
}

// --- Material ---
//
// BasicMaterial is unlit, so Deferred and Lighting are nil and it always
// renders through Forward.
// Every method below is a plain store lookup. They are spelled out here, rather than
// inherited from a shared base, so that this file is the whole of BasicMaterial.

// Copy returns another handle to the same instance (refcount++). The material is its
// own record, so there is exactly one BasicMaterial per instance and this returns the
// same pointer — every handle observes the same fields.
func (m *BasicMaterial) Copy() Material {
	m.ref.Copy() // bumps the shared count; the ref.Ref it returns is identical to m.ref
	return m
}

// Release drops this handle's reference. At refcount 0 the store disposes the slot,
// which releases the textures the material bound.
func (m *BasicMaterial) Release() {
	m.ref.Release()
}

// Valid reports whether the underlying instance is still alive.
func (m *BasicMaterial) Valid() bool {
	return m.ref.Valid()
}

// Vertex is nil for the built-in materials: they use the default vertex-pull shader.
func (m *BasicMaterial) Vertex() []byte {
	return m.pool.Shader().Vertex
}

// Forward is the always-present single-pass shader (surface + lighting).
func (m *BasicMaterial) Forward() []byte {
	return m.pool.Shader().Forward
}

// Deferred fills the G-buffer; nil means this material always renders forward.
func (m *BasicMaterial) Deferred() []byte {
	return m.pool.Shader().Deferred
}

// Lighting shades the G-buffer in a fullscreen pass; nil means always forward.
func (m *BasicMaterial) Lighting() []byte {
	return m.pool.Shader().Lighting
}

// Cull reports which triangle faces are discarded.
func (m *BasicMaterial) Cull() CullMode {
	return m.pool.Cull(m.ref.ID())
}

// SetCull sets which faces are culled (CullNone = double-sided).
func (m *BasicMaterial) SetCull(mode CullMode) {
	m.pool.SetCull(m.ref.ID(), mode)
}

// SetDoubleSided is a convenience for SetCull(CullNone) / SetCull(CullBack).
func (m *BasicMaterial) SetDoubleSided(enabled bool) {
	if enabled {
		m.SetCull(CullNone)
	} else {
		m.SetCull(CullBack)
	}
}

// Blend reports the material's blend mode. Anything but BlendOpaque also pins the
// material to the forward path — the G-buffer holds one surface per pixel, so it
// cannot represent a fragment that composites over what is behind it.
func (m *BasicMaterial) Blend() BlendMode {
	return m.pool.Blend(m.ref.ID())
}

// SetBlend sets the material's blend mode (Opaque/Alpha/Additive).
func (m *BasicMaterial) SetBlend(mode BlendMode) {
	m.pool.SetBlend(m.ref.ID(), mode)
}

// Pool returns the pool this material's records live in — its shader, pipeline
// identity and record buffer.
func (m *BasicMaterial) Pool() *Pool {
	return m.pool
}

// Hash is this material type's pipeline identity: its pool's, since these
// materials vary only by shader.
func (m *BasicMaterial) Hash() uint32 {
	return m.pool.Hash()
}

// ID is the instance's index within its store; RecordsAddr is the store's
// record buffer address, resolved at draw time because it moves when the store grows.
func (m *BasicMaterial) ID() uint32 {
	return m.ref.ID()
}

func (m *BasicMaterial) RecordsAddr() uint64 {
	return m.pool.RecordsAddr()
}
