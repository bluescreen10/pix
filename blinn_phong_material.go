package pix

import (
	_ "embed"
	"unsafe"

	"github.com/bluescreen10/pix/colors"
)

//go:embed shaders/build/scene_lit.frag.spv
var blinnPhongForwardSPV []byte

// BlinnPhongMaterial is ambient + diffuse (N·L) + Blinn-Phong specular.
//
// The material is its own GPU record: these fields are the source of truth and Bytes
// serializes them on demand. Every setter must call dirty(), or the change is never
// uploaded.
type BlinnPhongMaterial struct {
	store *materialStore
	ref   Ref

	emissive  colors.RGB32F
	specular  float32
	shininess float32

	// Bound maps. The material holds the reference that keeps each texture alive; the
	// record's bindless index and presence flag are derived from it in Bytes, so they
	// cannot fall out of step with what is actually bound.
	color        colors.RGBA32F
	colorMap     Texture
	colorSampler uint32
}

// NewBlinnPhongMaterial creates a Blinn-Phong material with an unbound color map.
// Defaults to white with a soft highlight.
func (r *Renderer) NewBlinnPhongMaterial() *BlinnPhongMaterial {
	// Forward-only for now: a deferred path means a Deferred pass that packs
	// (specular, shininess) into the G-buffer's model-defined material channel, plus a
	// matching Lighting pass. See shaders/src/gbuffer.glsl.
	st := r.materials.store(Shader{Forward: blinnPhongForwardSPV}, "BlinnPhong Material")
	m := &BlinnPhongMaterial{color: colors.RGBA32F{1, 1, 1, 1}, specular: 0.3, shininess: 32}
	m.store = st
	m.ref = st.register(m)
	return m
}

// Bytes implements materialInstance: the 64-byte record matching the Material struct
// in shaders/src/scene_lit.frag.glsl. Field order and padding here ARE the GPU layout —
// changing either without changing the shader silently misreads every material.
func (m *BlinnPhongMaterial) Bytes() []byte {
	rec := struct {
		color        colors.RGBA32F
		emissive     colors.RGB32F
		_            float32 // the shader declares vec4; the 4th channel is unused
		specular     float32
		shininess    float32
		colorMap     uint32
		colorSampler uint32
		flags        uint32
		_, _, _      uint32
	}{
		color:        m.color,
		emissive:     m.emissive,
		specular:     m.specular,
		shininess:    m.shininess,
		colorMap:     mapIndex(m.colorMap),
		colorSampler: m.colorSampler,
		flags:        mapFlag(m.colorMap, MatColorMap),
	}
	return unsafe.Slice((*byte)(unsafe.Pointer(&rec)), unsafe.Sizeof(rec))
}

// release implements materialInstance: drop every texture this material bound.
func (m *BlinnPhongMaterial) release() {
	m.colorMap.Release()
}

// dirty marks the record for re-upload in the next Sync. Every setter must call it;
// one that does not leaves the GPU rendering the previous value indefinitely.
func (m *BlinnPhongMaterial) dirty() {
	m.store.markDirty(m.ref.id)
}

func (m *BlinnPhongMaterial) Color() colors.RGBA32F {
	return m.color
}

func (m *BlinnPhongMaterial) SetColor(color colors.RGBA32F) {
	m.color = color
	m.dirty()
}

func (m *BlinnPhongMaterial) Specular() float32 {
	return m.specular
}

func (m *BlinnPhongMaterial) SetSpecular(specular float32) {
	m.specular = specular
	m.dirty()
}

func (m *BlinnPhongMaterial) Shininess() float32 {
	return m.shininess
}

func (m *BlinnPhongMaterial) SetShininess(shininess float32) {
	m.shininess = shininess
	m.dirty()
}

// Emissive is the light the surface emits on its own, added after lighting. It has no
// alpha: emitted light is not a coverage, and the record's fourth channel is padding
// the shader never reads.
func (m *BlinnPhongMaterial) Emissive() colors.RGB32F {
	return m.emissive
}

func (m *BlinnPhongMaterial) SetEmissive(color colors.RGB32F) {
	m.emissive = color
	m.dirty()
}

// The bound textures (or a zero handle).
func (m *BlinnPhongMaterial) ColorMap() Texture {
	return m.colorMap
}

// SetColorMap binds the base-color map; pass a zero Texture to clear it. The material
// takes its own reference, so the caller may release theirs.
func (m *BlinnPhongMaterial) SetColorMap(texture Texture) {
	old := m.colorMap
	m.colorMap = texture.Copy()
	old.Release() // after the copy, so rebinding a texture to itself cannot free it
	m.dirty()
}

// Per-map sampler getters/setters (bindless sampler heap indices).
func (m *BlinnPhongMaterial) ColorMapSampler() uint32 {
	return m.colorSampler
}

func (m *BlinnPhongMaterial) SetColorMapSampler(sampler uint32) {
	m.colorSampler = sampler
	m.dirty()
}

// --- Material ---
//
// BlinnPhongMaterial has no deferred path yet, so Deferred and Lighting are nil
// and it always renders through Forward.
// Every method below is a plain store lookup. They are spelled out here, rather than
// inherited from a shared base, so that this file is the whole of BlinnPhongMaterial.

// Copy returns another handle to the same instance (refcount++). The material is its
// own record, so there is exactly one BlinnPhongMaterial per instance and this returns
// the same pointer — every handle observes the same fields.
func (m *BlinnPhongMaterial) Copy() Material {
	m.ref.Copy() // bumps the shared count; the Ref it returns is identical to m.ref
	return m
}

// Release drops this handle's reference. At refcount 0 the store disposes the slot,
// which releases the textures the material bound.
func (m *BlinnPhongMaterial) Release() {
	m.ref.Release()
}

// Valid reports whether the underlying instance is still alive.
func (m *BlinnPhongMaterial) Valid() bool {
	return m.ref.Valid()
}

// Vertex is nil for the built-in materials: they use the default vertex-pull shader.
func (m *BlinnPhongMaterial) Vertex() []byte {
	return m.store.shader().Vertex
}

// Forward is the always-present single-pass shader (surface + lighting).
func (m *BlinnPhongMaterial) Forward() []byte {
	return m.store.shader().Forward
}

// Deferred fills the G-buffer; nil means this material always renders forward.
func (m *BlinnPhongMaterial) Deferred() []byte {
	return m.store.shader().Deferred
}

// Lighting shades the G-buffer in a fullscreen pass; nil means always forward.
func (m *BlinnPhongMaterial) Lighting() []byte {
	return m.store.shader().Lighting
}

// Cull reports which triangle faces are discarded.
func (m *BlinnPhongMaterial) Cull() CullMode {
	return m.store.cullOf(m.ref.id)
}

// SetCull sets which faces are culled (CullNone = double-sided).
func (m *BlinnPhongMaterial) SetCull(mode CullMode) {
	m.store.setCullOf(m.ref.id, mode)
}

// SetDoubleSided is a convenience for SetCull(CullNone) / SetCull(CullBack).
func (m *BlinnPhongMaterial) SetDoubleSided(enabled bool) {
	if enabled {
		m.SetCull(CullNone)
	} else {
		m.SetCull(CullBack)
	}
}

// Blend reports the material's blend mode. Anything but BlendOpaque also pins the
// material to the forward path — the G-buffer holds one surface per pixel, so it
// cannot represent a fragment that composites over what is behind it.
func (m *BlinnPhongMaterial) Blend() BlendMode {
	return m.store.blendOf(m.ref.id)
}

// SetBlend sets the material's blend mode (Opaque/Alpha/Additive).
func (m *BlinnPhongMaterial) SetBlend(mode BlendMode) {
	m.store.setBlendOf(m.ref.id, mode)
}

// Cached pipeline identities, precomputed per store (see the Material interface).
func (m *BlinnPhongMaterial) shaderHash() uint32 {
	return m.store.pipeHash
}

func (m *BlinnPhongMaterial) deferredHash() uint32 {
	return m.store.deferHash
}

func (m *BlinnPhongMaterial) lightingHash() uint32 {
	return m.store.lightHash
}

// materialID is the instance's index within its store; materialsAddr is the store's
// record buffer address, resolved at draw time because it moves when the store grows.
func (m *BlinnPhongMaterial) materialID() uint32 {
	return m.ref.id
}

func (m *BlinnPhongMaterial) materialsAddr() uint64 {
	return m.store.bufAddr()
}
