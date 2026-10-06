package materials

import (
	"unsafe"

	"github.com/bluescreen10/pix/colors"
	"github.com/bluescreen10/pix/ref"
	"github.com/bluescreen10/pix/shaders"
	"github.com/bluescreen10/pix/textures"
)

// PBRMaterial is a metallic-roughness physically-based material (Cook-Torrance) with
// optional base-color, normal, metallic, roughness, occlusion and transmission maps.
//
// The material is its own GPU record: these fields are the source of truth and Bytes
// serializes them on demand. Every setter must call dirty(), or the change is never
// uploaded.
type PBRMaterial struct {
	pool *Pool
	ref  ref.Ref

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

	// occlusionMap is how much of the light around it reaches each texel, baked into the
	// map's red channel (glTF's occlusionTexture): it darkens ambient and environment
	// light, never direct light. occlusionStrength is how far: 0 not at all, 1 fully.
	occlusionStrength float32
	occlusionMap      textures.Texture
	occlusionSampler  uint32

	// alphaCutoff makes the material masked: where its colour's alpha falls below it,
	// there is no surface (see Masked). 0 keeps every texel.
	alphaCutoff float32
}

// NewPBRMaterial creates a PBR material with no maps bound.
func NewPBRMaterial(store *Store) *PBRMaterial {
	st := store.Pool(Shader{Fragment: shaders.PBRFragment}, "PBR Material")
	sampler := store.DefaultSampler()
	m := &PBRMaterial{
		color: colors.RGBA32F{1, 1, 1, 1}, roughness: 0.5, occlusionStrength: 1,
		colorSampler: sampler, normalSampler: sampler, metallicSampler: sampler,
		roughnessSampler: sampler, transmissionSampler: sampler, occlusionSampler: sampler,
	}
	m.pool = st
	m.ref = st.Create(m)
	return m
}

// Bytes implements Instance: the 100-byte record matching the Material struct
// in shaders/src/scene_pbr.frag.glsl. Field order and padding here ARE the GPU layout —
// changing either without changing the shader silently misreads every material.
func (m *PBRMaterial) Bytes() []byte {
	rec := struct {
		color             colors.RGBA32F
		emissive          colors.RGB32F
		_                 float32 // the shader declares vec4; the 4th channel is unused
		metallic          float32
		roughness         float32
		transmission      float32
		flags             uint32
		colorMap          uint32
		colorSampler      uint32
		normalMap         uint32
		normalSampler     uint32
		metallicMap       uint32
		metallicSampler   uint32
		roughnessMap      uint32
		roughnessSampler  uint32
		transMap          uint32
		transSampler      uint32
		occlusionStrength float32
		occlusionMap      uint32
		occlusionSampler  uint32
	}{
		color:        m.color,
		emissive:     m.emissive,
		metallic:     m.metallic,
		roughness:    m.roughness,
		transmission: m.transmission,
		flags: MapFlag(m.colorMap, MatColorMap) | MapFlag(m.normalMap, MatNormalMap) |
			MapFlag(m.metallicMap, MatMetalMap) | MapFlag(m.roughnessMap, MatRoughMap) |
			MapFlag(m.transmissionMap, MatTransMap) | MapFlag(m.occlusionMap, MatOcclusionMap),
		colorMap: MapIndex(m.colorMap), colorSampler: m.colorSampler,
		normalMap: MapIndex(m.normalMap), normalSampler: m.normalSampler,
		metallicMap: MapIndex(m.metallicMap), metallicSampler: m.metallicSampler,
		roughnessMap: MapIndex(m.roughnessMap), roughnessSampler: m.roughnessSampler,
		transMap: MapIndex(m.transmissionMap), transSampler: m.transmissionSampler,
		occlusionStrength: m.occlusionStrength,
		occlusionMap:      MapIndex(m.occlusionMap), occlusionSampler: m.occlusionSampler,
	}
	return unsafe.Slice((*byte)(unsafe.Pointer(&rec)), unsafe.Sizeof(rec))
}

// Dispose implements Instance: drop every texture this material bound.
func (m *PBRMaterial) Dispose() {
	m.colorMap.Release()
	m.normalMap.Release()
	m.metallicMap.Release()
	m.roughnessMap.Release()
	m.transmissionMap.Release()
	m.occlusionMap.Release()
}

// dirty marks the record for re-upload in the next Sync. Every setter must call it;
// one that does not leaves the GPU rendering the previous value indefinitely.
func (m *PBRMaterial) dirty() {
	m.pool.MarkDirty(m.ref.ID())
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
func (m *PBRMaterial) TransmissionMap() textures.Texture {
	return m.transmissionMap
}

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

// OcclusionMap returns the bound occlusion map.
func (m *PBRMaterial) OcclusionMap() textures.Texture {
	return m.occlusionMap
}

// SetOcclusionMap binds a map of how much of the light around it reaches each texel, in
// its red channel (glTF's occlusionTexture): baked occlusion, which darkens ambient and
// environment light and leaves direct light alone. Packed with roughness and metalness,
// as glTF's occlusion-roughness-metallic textures are, the same texture can be bound to
// all three maps.
func (m *PBRMaterial) SetOcclusionMap(texture textures.Texture) {
	old := m.occlusionMap
	m.occlusionMap = texture.Copy()
	old.Release() // after the copy, so rebinding a texture to itself cannot free it
	m.dirty()
}

func (m *PBRMaterial) OcclusionMapSampler() uint32 {
	return m.occlusionSampler
}

func (m *PBRMaterial) SetOcclusionMapSampler(sampler uint32) {
	m.occlusionSampler = sampler
	m.dirty()
}

// OcclusionStrength is how far the occlusion map darkens: 0 not at all, 1 fully, the
// default (glTF's occlusionTexture.strength).
func (m *PBRMaterial) OcclusionStrength() float32 {
	return m.occlusionStrength
}

func (m *PBRMaterial) SetOcclusionStrength(strength float32) {
	m.occlusionStrength = strength
	m.dirty()
}

// AlphaCutoff is the alpha below which the material has no surface; 0, the default,
// keeps all of it.
func (m *PBRMaterial) AlphaCutoff() float32 {
	return m.alphaCutoff
}

// SetAlphaCutoff makes the material masked: where its colour's alpha — the colour map's
// times the colour's own — falls below cutoff, there is no surface, in shading, shadows
// and depth alike (glTF's alphaMode MASK, whose alphaCutoff defaults to 0.5). 0 makes it
// whole again.
func (m *PBRMaterial) SetAlphaCutoff(cutoff float32) {
	m.alphaCutoff = cutoff
	m.dirty()
}

// AlphaMask implements Masked: the material cuts its surface out by its colour's alpha
// once it has a cut-off.
func (m *PBRMaterial) AlphaMask() (AlphaMask, bool) {
	mask := AlphaMask{Map: m.colorMap, Sampler: m.colorSampler, Alpha: m.color[3], Cutoff: m.alphaCutoff}
	return mask, m.alphaCutoff > 0
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
	m.ref.Copy() // bumps the shared count; the ref.Ref it returns is identical to m.ref
	return m
}

// Release drops this handle's reference. At refcount 0 the store disposes the slot,
// which releases the textures the material bound.
func (m *PBRMaterial) Release() {
	m.ref.Release()
}

// Valid reports whether the underlying instance is still alive.
func (m *PBRMaterial) IsValid() bool {
	return m.ref.IsValid()
}

// Cull reports which triangle faces are discarded.
func (m *PBRMaterial) Cull() CullMode {
	return m.pool.CullAt(m.ref.ID())
}

// SetCull sets which faces are culled (CullNone = double-sided).
func (m *PBRMaterial) SetCull(mode CullMode) {
	m.pool.SetCullAt(m.ref.ID(), mode)
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
	return m.pool.BlendAt(m.ref.ID())
}

// SetBlend sets the material's blend mode (Opaque/Alpha/Additive).
func (m *PBRMaterial) SetBlend(mode BlendMode) {
	m.pool.SetBlendAt(m.ref.ID(), mode)
}

// Pool returns the pool this material's records live in — its shader, pipeline
// identity and record buffer.
func (m *PBRMaterial) Pool() *Pool {
	return m.pool
}

// ID is the instance's index within its store; RecordsAddr is the store's
// record buffer address, resolved at draw time because it moves when the store grows.
func (m *PBRMaterial) ID() ID {
	return m.pool.IDOf(m.ref)
}
