package pix

import (
	_ "embed"
	"unsafe"

	"github.com/bluescreen10/pix/glm"
)

//go:embed shaders/build/scene_forward_pbr.frag.spv
var pbrForwardSPV []byte

//go:embed shaders/build/scene_deferred_pbr.frag.spv
var pbrDeferredSPV []byte

//go:embed shaders/build/scene_lighting_pbr.frag.spv
var pbrLightingSPV []byte

// PBRMaterial is a metallic-roughness physically-based material (Cook-Torrance) with
// optional base-color, normal, metallic and roughness maps.
//
// The material is its own GPU record: these fields are the source of truth and Bytes
// serializes them on demand. Every setter must call dirty(), or the change is never
// uploaded.
type PBRMaterial struct {
	store *materialStore
	ref   Ref

	emissive     glm.RGBA32F
	transmission float32 // 0 = opaque; >0 = see-through (glass), needs alpha blend

	// Bound maps. The material holds the reference that keeps each texture alive; the
	// record's bindless index and presence flag are derived from it in Bytes, so they
	// cannot fall out of step with what is actually bound.
	color        glm.RGBA32F
	colorMap     Texture
	colorSampler uint32

	normalMap     Texture
	normalSampler uint32

	metallic        float32
	metallicMap     Texture
	metallicSampler uint32

	roughness        float32
	roughnessMap     Texture
	roughnessSampler uint32
}

// NewPBRMaterial creates a PBR material with four unbound maps: color, normal,
// metallic and roughness.
func (r *Renderer) NewPBRMaterial() *PBRMaterial {
	st := r.materials.store(
		Shader{Forward: pbrForwardSPV, Deferred: pbrDeferredSPV, Lighting: pbrLightingSPV},
		"PBR Material",
	)
	m := &PBRMaterial{color: glm.RGBA32F{1, 1, 1, 1}, roughness: 0.5}
	m.store = st
	m.ref = st.register(m)
	return m
}

// Bytes implements materialInstance: the 80-byte record matching the Material struct
// in shaders/src/scene_pbr.frag.glsl. Field order and padding here ARE the GPU layout —
// changing either without changing the shader silently misreads every material.
func (m *PBRMaterial) Bytes() []byte {
	rec := struct {
		color            glm.RGBA32F
		emissive         glm.RGBA32F
		metallic         float32
		roughness        float32
		transmission     float32
		flags            uint32
		colorMap         uint32
		colorSampler     uint32
		normalMap        uint32
		normalSampler    uint32
		metallicMap      uint32
		metallicSampler  uint32
		roughnessMap     uint32
		roughnessSampler uint32
	}{
		color:        m.color,
		emissive:     m.emissive,
		metallic:     m.metallic,
		roughness:    m.roughness,
		transmission: m.transmission,
		flags: mapFlag(m.colorMap, MatColorMap) | mapFlag(m.normalMap, MatNormalMap) |
			mapFlag(m.metallicMap, MatMetalMap) | mapFlag(m.roughnessMap, MatRoughMap),
		colorMap: mapIndex(m.colorMap), colorSampler: m.colorSampler,
		normalMap: mapIndex(m.normalMap), normalSampler: m.normalSampler,
		metallicMap: mapIndex(m.metallicMap), metallicSampler: m.metallicSampler,
		roughnessMap: mapIndex(m.roughnessMap), roughnessSampler: m.roughnessSampler,
	}
	return unsafe.Slice((*byte)(unsafe.Pointer(&rec)), unsafe.Sizeof(rec))
}

// release implements materialInstance: drop every texture this material bound.
func (m *PBRMaterial) release() {
	m.colorMap.Release()
	m.normalMap.Release()
	m.metallicMap.Release()
	m.roughnessMap.Release()
}

// dirty marks the record for re-upload in the next Sync. Every setter must call it;
// one that does not leaves the GPU rendering the previous value indefinitely.
func (m *PBRMaterial) dirty() {
	m.store.markDirty(m.ref.id)
}

func (m *PBRMaterial) Color() glm.RGBA32F {
	return m.color
}

func (m *PBRMaterial) SetColor(color glm.RGBA32F) {
	m.color = color
	m.dirty()
}

func (m *PBRMaterial) Metallic() float32 {
	return m.metallic
}

func (m *PBRMaterial) SetMetallic(v float32) {
	m.metallic = v
	m.dirty()
}

func (m *PBRMaterial) Roughness() float32 {
	return m.roughness
}

func (m *PBRMaterial) SetRoughness(v float32) {
	m.roughness = v
	m.dirty()
}

// Transmission returns the transmission factor [0,1] (glass-like see-through).
func (m *PBRMaterial) Transmission() float32 {
	return m.transmission
}

// SetTransmission sets how much light passes through the surface [0,1]. A value > 0
// makes the material transparent (approximate glass) and switches it to alpha
// blending, which also keeps it on the forward path — the G-buffer has nowhere to
// store transmission, so a transmissive material left opaque would render as solid
// once deferred rendering is enabled. Never switches back to opaque on its own; call
// SetBlend(BlendOpaque) explicitly if you clear transmission.
func (m *PBRMaterial) SetTransmission(v float32) {
	m.transmission = v
	m.dirty()
	if v > 0 && m.Blend() == BlendOpaque {
		m.SetBlend(BlendAlpha)
	}
}

func (m *PBRMaterial) Emissive() glm.RGBA32F {
	return m.emissive
}

func (m *PBRMaterial) SetEmissive(color glm.RGBA32F) {
	m.emissive = color
	m.dirty()
}

// The bound textures (or a zero handle).
func (m *PBRMaterial) ColorMap() Texture {
	return m.colorMap
}

// SetColorMap binds the base-color (albedo) map; pass a zero Texture to clear it. The
// material takes its own reference, so the caller may release theirs.
func (m *PBRMaterial) SetColorMap(t Texture) {
	old := m.colorMap
	m.colorMap = t.Copy()
	old.Release() // after the copy, so rebinding a texture to itself cannot free it
	m.dirty()
}

// Per-map sampler getters/setters (bindless sampler heap indices).
func (m *PBRMaterial) ColorMapSampler() uint32 {
	return m.colorSampler
}

func (m *PBRMaterial) SetColorMapSampler(s uint32) {
	m.colorSampler = s
	m.dirty()
}

func (m *PBRMaterial) NormalMap() Texture {
	return m.normalMap
}

// SetNormalMap binds a tangent-space normal map (linear RGB, xyz in [0,1]).
func (m *PBRMaterial) SetNormalMap(t Texture) {
	old := m.normalMap
	m.normalMap = t.Copy()
	old.Release() // after the copy, so rebinding a texture to itself cannot free it
	m.dirty()
}

func (m *PBRMaterial) NormalMapSampler() uint32 {
	return m.normalSampler
}

func (m *PBRMaterial) SetNormalMapSampler(s uint32) {
	m.normalSampler = s
	m.dirty()
}

func (m *PBRMaterial) MetallicMap() Texture {
	return m.metallicMap
}

// SetMetallicMap binds a metallic map; its blue channel modulates the metallic factor
// (matches the glTF metallic-roughness texture convention, and works for grayscale).
func (m *PBRMaterial) SetMetallicMap(t Texture) {
	old := m.metallicMap
	m.metallicMap = t.Copy()
	old.Release() // after the copy, so rebinding a texture to itself cannot free it
	m.dirty()
}

func (m *PBRMaterial) MetallicMapSampler() uint32 {
	return m.metallicSampler
}

func (m *PBRMaterial) SetMetallicMapSampler(s uint32) {
	m.metallicSampler = s
	m.dirty()
}

func (m *PBRMaterial) RoughnessMap() Texture {
	return m.roughnessMap
}

// SetRoughnessMap binds a roughness map; its green channel modulates the roughness
// factor (glTF convention; works for grayscale too).
func (m *PBRMaterial) SetRoughnessMap(t Texture) {
	old := m.roughnessMap
	m.roughnessMap = t.Copy()
	old.Release() // after the copy, so rebinding a texture to itself cannot free it
	m.dirty()
}

func (m *PBRMaterial) RoughnessMapSampler() uint32 {
	return m.roughnessSampler
}

func (m *PBRMaterial) SetRoughnessMapSampler(s uint32) {
	m.roughnessSampler = s
	m.dirty()
}

// --- Material ---
//
// PBRMaterial supplies all three passes, so it renders through the G-buffer
// whenever deferred rendering is enabled and it is unblended.
// Every method below is a plain store lookup. They are spelled out here, rather than
// inherited from a shared base, so that this file is the whole of PBRMaterial.

// Copy returns another handle to the same instance (refcount++). The material is its
// own record, so there is exactly one PBRMaterial per instance and this returns the
// same pointer — every handle observes the same fields and the same bound textures.
func (m *PBRMaterial) Copy() Material {
	m.ref.Copy() // bumps the shared count; the Ref it returns is identical to m.ref
	return m
}

// Release drops this handle's reference. At refcount 0 the store disposes the slot,
// which releases the textures the material bound.
func (m *PBRMaterial) Release() {
	m.ref.Release()
}

// Valid reports whether the underlying instance is still alive.
func (m *PBRMaterial) Valid() bool {
	return m.ref.Valid()
}

// Vertex is nil for the built-in materials: they use the default vertex-pull shader.
func (m *PBRMaterial) Vertex() []byte {
	return m.store.shader().Vertex
}

// Forward is the always-present single-pass shader (surface + lighting).
func (m *PBRMaterial) Forward() []byte {
	return m.store.shader().Forward
}

// Deferred fills the G-buffer; nil means this material always renders forward.
func (m *PBRMaterial) Deferred() []byte {
	return m.store.shader().Deferred
}

// Lighting shades the G-buffer in a fullscreen pass; nil means always forward.
func (m *PBRMaterial) Lighting() []byte {
	return m.store.shader().Lighting
}

// Cull reports which triangle faces are discarded.
func (m *PBRMaterial) Cull() CullMode {
	return m.store.cullOf(m.ref.id)
}

// SetCull sets which faces are culled (CullNone = double-sided).
func (m *PBRMaterial) SetCull(c CullMode) {
	m.store.setCullOf(m.ref.id, c)
}

// SetDoubleSided is a convenience for SetCull(CullNone) / SetCull(CullBack).
func (m *PBRMaterial) SetDoubleSided(v bool) {
	if v {
		m.SetCull(CullNone)
	} else {
		m.SetCull(CullBack)
	}
}

// Blend reports the material's blend mode. Anything but BlendOpaque also pins the
// material to the forward path — the G-buffer holds one surface per pixel, so it
// cannot represent a fragment that composites over what is behind it.
func (m *PBRMaterial) Blend() BlendMode {
	return m.store.blendOf(m.ref.id)
}

// SetBlend sets the material's blend mode (Opaque/Alpha/Additive).
func (m *PBRMaterial) SetBlend(b BlendMode) {
	m.store.setBlendOf(m.ref.id, b)
}

// Cached pipeline identities, precomputed per store (see the Material interface).
func (m *PBRMaterial) shaderHash() uint32 {
	return m.store.pipeHash
}

func (m *PBRMaterial) deferredHash() uint32 {
	return m.store.deferHash
}

func (m *PBRMaterial) lightingHash() uint32 {
	return m.store.lightHash
}

// materialID is the instance's index within its store; materialsAddr is the store's
// record buffer address, resolved at draw time because it moves when the store grows.
func (m *PBRMaterial) materialID() uint32 {
	return m.ref.id
}

func (m *PBRMaterial) materialsAddr() uint64 {
	return m.store.bufAddr()
}
