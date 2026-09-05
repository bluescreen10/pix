package pix

import (
	_ "embed"
	"unsafe"

	"github.com/bluescreen10/pix/colors"
	"github.com/bluescreen10/pix/textures"
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

	emissive     colors.RGB32F
	transmission float32 // 0 = opaque; >0 = see-through (glass), needs alpha blend

	// Bound maps. The material holds the reference that keeps each texture alive; the
	// record's bindless index and presence flag are derived from it in Bytes, so they
	// cannot fall out of step with what is actually bound.
	color        colors.RGBA32F
	colorMap     textures.Texture
	colorSampler uint32

	normalMap     textures.Texture
	normalSampler uint32

	metallic        float32
	metallicMap     textures.Texture
	metallicSampler uint32

	roughness        float32
	roughnessMap     textures.Texture
	roughnessSampler uint32

	// transmissionMap scales transmission per texel (glTF KHR_materials_transmission
	// stores it in red). Without it a partly-glass object — a cabinet with panes, a
	// sign with a glass front — becomes uniformly transparent.
	transmissionMap     textures.Texture
	transmissionSampler uint32
}

// NewPBRMaterial creates a PBR material with four unbound maps: color, normal,
// metallic and roughness.
func (r *Renderer) NewPBRMaterial() *PBRMaterial {
	st := r.materials.store(
		Shader{Forward: pbrForwardSPV, Deferred: pbrDeferredSPV, Lighting: pbrLightingSPV},
		"PBR Material",
	)
	m := &PBRMaterial{color: colors.RGBA32F{1, 1, 1, 1}, roughness: 0.5}
	m.store = st
	m.ref = st.register(m)
	return m
}

// Bytes implements materialInstance: the 88-byte record matching the Material struct
// in shaders/src/scene_pbr.frag.glsl. Field order and padding here ARE the GPU layout —
// changing either without changing the shader silently misreads every material.
func (m *PBRMaterial) Bytes() []byte {
	rec := struct {
		color            colors.RGBA32F
		emissive         colors.RGB32F
		_                float32 // the shader declares vec4; the 4th channel is unused
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
		transMap         uint32
		transSampler     uint32
	}{
		color:        m.color,
		emissive:     m.emissive,
		metallic:     m.metallic,
		roughness:    m.roughness,
		transmission: m.transmission,
		flags: mapFlag(m.colorMap, MatColorMap) | mapFlag(m.normalMap, MatNormalMap) |
			mapFlag(m.metallicMap, MatMetalMap) | mapFlag(m.roughnessMap, MatRoughMap) |
			mapFlag(m.transmissionMap, MatTransMap),
		colorMap: mapIndex(m.colorMap), colorSampler: m.colorSampler,
		normalMap: mapIndex(m.normalMap), normalSampler: m.normalSampler,
		metallicMap: mapIndex(m.metallicMap), metallicSampler: m.metallicSampler,
		roughnessMap: mapIndex(m.roughnessMap), roughnessSampler: m.roughnessSampler,
		transMap: mapIndex(m.transmissionMap), transSampler: m.transmissionSampler,
	}
	return unsafe.Slice((*byte)(unsafe.Pointer(&rec)), unsafe.Sizeof(rec))
}

// release implements materialInstance: drop every texture this material bound.
func (m *PBRMaterial) release() {
	m.colorMap.Release()
	m.normalMap.Release()
	m.metallicMap.Release()
	m.roughnessMap.Release()
	m.transmissionMap.Release()
}

// dirty marks the record for re-upload in the next Sync. Every setter must call it;
// one that does not leaves the GPU rendering the previous value indefinitely.
func (m *PBRMaterial) dirty() {
	m.store.markDirty(m.ref.id)
}

func (m *PBRMaterial) Color() colors.RGBA32F {
	return m.color
}

func (m *PBRMaterial) SetColor(color colors.RGBA32F) {
	m.color = color
	m.dirty()
}

func (m *PBRMaterial) Metallic() float32 {
	return m.metallic
}

func (m *PBRMaterial) SetMetallic(metallic float32) {
	m.metallic = metallic
	m.dirty()
}

func (m *PBRMaterial) Roughness() float32 {
	return m.roughness
}

func (m *PBRMaterial) SetRoughness(roughness float32) {
	m.roughness = roughness
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
func (m *PBRMaterial) SetTransmission(transmission float32) {
	m.transmission = transmission
	m.dirty()
	if transmission > 0 && m.Blend() == BlendOpaque {
		m.SetBlend(BlendAlpha)
	}
}

// Emissive is the light the surface emits on its own, added after lighting. It has no
// alpha: emitted light is not a coverage, and the record's fourth channel is padding
// the shader never reads.
func (m *PBRMaterial) Emissive() colors.RGB32F {
	return m.emissive
}

func (m *PBRMaterial) SetEmissive(color colors.RGB32F) {
	m.emissive = color
	m.dirty()
}

// The bound textures (or a zero handle).
func (m *PBRMaterial) ColorMap() textures.Texture {
	return m.colorMap
}

// SetColorMap binds the base-color (albedo) map; pass a zero textures.Texture to clear it. The
// material takes its own reference, so the caller may release theirs.
func (m *PBRMaterial) SetColorMap(texture textures.Texture) {
	old := m.colorMap
	m.colorMap = texture.Copy()
	old.Release() // after the copy, so rebinding a texture to itself cannot free it
	m.dirty()
}

// Per-map sampler getters/setters (bindless sampler heap indices).
func (m *PBRMaterial) ColorMapSampler() uint32 {
	return m.colorSampler
}

func (m *PBRMaterial) SetColorMapSampler(sampler uint32) {
	m.colorSampler = sampler
	m.dirty()
}

func (m *PBRMaterial) NormalMap() textures.Texture {
	return m.normalMap
}

// SetNormalMap binds a tangent-space normal map (linear RGB, xyz in [0,1]).
func (m *PBRMaterial) SetNormalMap(texture textures.Texture) {
	old := m.normalMap
	m.normalMap = texture.Copy()
	old.Release() // after the copy, so rebinding a texture to itself cannot free it
	m.dirty()
}

func (m *PBRMaterial) NormalMapSampler() uint32 {
	return m.normalSampler
}

func (m *PBRMaterial) SetNormalMapSampler(sampler uint32) {
	m.normalSampler = sampler
	m.dirty()
}

func (m *PBRMaterial) MetallicMap() textures.Texture {
	return m.metallicMap
}

// SetMetallicMap binds a metallic map; its blue channel modulates the metallic factor
// (matches the glTF metallic-roughness texture convention, and works for grayscale).
func (m *PBRMaterial) SetMetallicMap(texture textures.Texture) {
	old := m.metallicMap
	m.metallicMap = texture.Copy()
	old.Release() // after the copy, so rebinding a texture to itself cannot free it
	m.dirty()
}

func (m *PBRMaterial) MetallicMapSampler() uint32 {
	return m.metallicSampler
}

func (m *PBRMaterial) SetMetallicMapSampler(sampler uint32) {
	m.metallicSampler = sampler
	m.dirty()
}

func (m *PBRMaterial) RoughnessMap() textures.Texture {
	return m.roughnessMap
}

// SetRoughnessMap binds a roughness map; its green channel modulates the roughness
// factor (glTF convention; works for grayscale too).
func (m *PBRMaterial) SetRoughnessMap(texture textures.Texture) {
	old := m.roughnessMap
	m.roughnessMap = texture.Copy()
	old.Release() // after the copy, so rebinding a texture to itself cannot free it
	m.dirty()
}

func (m *PBRMaterial) RoughnessMapSampler() uint32 {
	return m.roughnessSampler
}

func (m *PBRMaterial) SetRoughnessMapSampler(sampler uint32) {
	m.roughnessSampler = sampler
	m.dirty()
}

// TransmissionMap returns the bound transmission map.
func (m *PBRMaterial) TransmissionMap() textures.Texture { return m.transmissionMap }

// SetTransmissionMap binds a per-texel transmission mask, multiplied with the scalar
// Transmission (glTF KHR_materials_transmission keeps it in the red channel). Use it
// when only part of a surface is glass — a cabinet's panes, a sign's window — since
// the scalar alone makes the whole object see-through.
func (m *PBRMaterial) SetTransmissionMap(texture textures.Texture) {
	newRef := texture.Copy()
	m.transmissionMap.Release()
	m.transmissionMap = newRef
	m.dirty()
}

// SetTransmissionMapSampler sets the heap index of the sampler used for the
// transmission map.
func (m *PBRMaterial) SetTransmissionMapSampler(sampler uint32) {
	m.transmissionSampler = sampler
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
func (m *PBRMaterial) SetCull(mode CullMode) {
	m.store.setCullOf(m.ref.id, mode)
}

// SetDoubleSided is a convenience for SetCull(CullNone) / SetCull(CullBack).
func (m *PBRMaterial) SetDoubleSided(enabled bool) {
	if enabled {
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
func (m *PBRMaterial) SetBlend(mode BlendMode) {
	m.store.setBlendOf(m.ref.id, mode)
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
