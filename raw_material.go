package pix

import "github.com/bluescreen10/pix/textures"

// RawMaterial is a self-describing, one-off material: you provide the shader and the
// per-instance data size, and the material system auto-allocates storage (a "uniform"
// buffer) from that — no hand-written typed store. Write the record bytes via Bytes()
// (interpret them however your fragment shader does). Ideal for particles or any
// custom material where a typed accessor struct isn't warranted.
type RawMaterial struct {
	store *materialStore
	ref   Ref

	// The material's own texture references, one per slot requested at construction.
	// These keep the textures alive; the record holds whatever bindless indices the
	// caller writes into it, which is all the shader reads.
	textures []textures.Texture

	// The record bytes. RawMaterial has no typed fields to serialize from, so it holds
	// the record itself and Bytes hands it straight to the store.
	data []byte
}

// NewRawMaterial creates a material whose fragment shader is `shader.Fragment` and
// whose per-instance record is dataSize bytes (with textureSlots bindless texture slots).
// Materials sharing a fragment shader share a store automatically.
func (r *Renderer) NewRawMaterial(shader Shader, dataSize, textureSlots int) *RawMaterial {
	st := r.materials.store(shader, "Custom Materials")
	m := &RawMaterial{
		textures: make([]textures.Texture, textureSlots),
		data:     make([]byte, dataSize),
	}
	m.store = st
	m.ref = st.register(m)
	return m
}

// Bytes implements materialInstance. It is the read side, used by Sync — call Record
// to write, or the change is never uploaded.
func (m *RawMaterial) Bytes() []byte { return m.data }

// Record returns the material's per-instance record as a mutable byte slice — write
// your uniform fields here — and marks it for upload in the next Sync. Interpret the
// bytes however your fragment shader does.
func (m *RawMaterial) Record() []byte {
	m.store.markDirty(m.ref.id)
	return m.data
}

// release implements materialInstance: drop every texture this material bound.
func (m *RawMaterial) release() {
	for _, t := range m.textures {
		t.Release()
	}
}

// SetTexture binds a texture into slot, taking a reference that lasts until the slot
// is rebound or the material is released. Write the texture's Index() into the record
// yourself (via Bytes) — RawMaterial has no typed record to do it for you.
func (m *RawMaterial) SetTexture(slot int, texture textures.Texture) {
	old := m.textures[slot]
	if texture.Valid() {
		m.textures[slot] = texture.Copy()
	} else {
		m.textures[slot] = textures.Texture{}
	}
	old.Release() // after the copy, so rebinding a texture to itself cannot free it
	m.store.markDirty(m.ref.id)
}

// textures.Texture returns the texture bound in slot (or a zero handle).
func (m *RawMaterial) Texture(slot int) textures.Texture { return m.textures[slot] }

// --- Material ---
//
// RawMaterial renders through whichever passes the caller supplied in its Shader.
// Every method below is a plain store lookup. They are spelled out here, rather than
// inherited from a shared base, so that this file is the whole of RawMaterial.

// Copy returns another handle to the same instance (refcount++). The material is its
// own record, so there is exactly one RawMaterial per instance and this returns the
// same pointer — every handle observes the same record and textures.
func (m *RawMaterial) Copy() Material {
	m.ref.Copy() // bumps the shared count; the Ref it returns is identical to m.ref
	return m
}

// Release drops this handle's reference. At refcount 0 the store disposes the slot,
// which releases the textures the material bound.
func (m *RawMaterial) Release() {
	m.ref.Release()
}

// Valid reports whether the underlying instance is still alive.
func (m *RawMaterial) Valid() bool {
	return m.ref.Valid()
}

// Vertex is nil for the built-in materials: they use the default vertex-pull shader.
func (m *RawMaterial) Vertex() []byte {
	return m.store.shader().Vertex
}

// Forward is the always-present single-pass shader (surface + lighting).
func (m *RawMaterial) Forward() []byte {
	return m.store.shader().Forward
}

// Deferred fills the G-buffer; nil means this material always renders forward.
func (m *RawMaterial) Deferred() []byte {
	return m.store.shader().Deferred
}

// Lighting shades the G-buffer in a fullscreen pass; nil means always forward.
func (m *RawMaterial) Lighting() []byte {
	return m.store.shader().Lighting
}

// Cull reports which triangle faces are discarded.
func (m *RawMaterial) Cull() CullMode {
	return m.store.cullOf(m.ref.id)
}

// SetCull sets which faces are culled (CullNone = double-sided).
func (m *RawMaterial) SetCull(mode CullMode) {
	m.store.setCullOf(m.ref.id, mode)
}

// SetDoubleSided is a convenience for SetCull(CullNone) / SetCull(CullBack).
func (m *RawMaterial) SetDoubleSided(enabled bool) {
	if enabled {
		m.SetCull(CullNone)
	} else {
		m.SetCull(CullBack)
	}
}

// Blend reports the material's blend mode. Anything but BlendOpaque also pins the
// material to the forward path — the G-buffer holds one surface per pixel, so it
// cannot represent a fragment that composites over what is behind it.
func (m *RawMaterial) Blend() BlendMode {
	return m.store.blendOf(m.ref.id)
}

// SetBlend sets the material's blend mode (Opaque/Alpha/Additive).
func (m *RawMaterial) SetBlend(mode BlendMode) {
	m.store.setBlendOf(m.ref.id, mode)
}

// Cached pipeline identities, precomputed per store (see the Material interface).
func (m *RawMaterial) shaderHash() uint32 {
	return m.store.pipeHash
}

func (m *RawMaterial) deferredHash() uint32 {
	return m.store.deferHash
}

func (m *RawMaterial) lightingHash() uint32 {
	return m.store.lightHash
}

// materialID is the instance's index within its store; materialsAddr is the store's
// record buffer address, resolved at draw time because it moves when the store grows.
func (m *RawMaterial) materialID() uint32 {
	return m.ref.id
}

func (m *RawMaterial) materialsAddr() uint64 {
	return m.store.bufAddr()
}
