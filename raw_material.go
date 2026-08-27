package pix

// RawMaterial is a self-describing, one-off material: you provide the shader and the
// per-instance data size, and the material system auto-allocates storage (a "uniform"
// buffer) from that — no hand-written typed store. Write the record bytes via Bytes()
// (interpret them however your fragment shader does). Ideal for particles or any
// custom material where a typed accessor struct isn't warranted.
type RawMaterial struct {
	genericMaterial
}

// NewRawMaterial creates a material whose fragment shader is `shader.Fragment` and
// whose per-instance record is dataSize bytes (with texSlots bindless texture slots).
// Materials sharing a fragment shader share a store automatically.
func (r *Renderer) NewRawMaterial(shader Shader, dataSize, texSlots int) *RawMaterial {
	st := r.mats.store(shader, uint32(dataSize), texSlots, "Custom Materials")
	id := st.alloc()
	rc := int32(1)
	return &RawMaterial{genericMaterial{store: st, ref: Ref{id: id, gen: st.gens[id], refCount: &rc, owner: st}}}
}

// Bytes returns the material's per-instance record as a mutable byte slice, mapped
// directly into GPU-visible memory — write your uniform fields here.
func (m *RawMaterial) Bytes() []byte { return m.store.dataOf(m.ref.id) }

// SetTexture binds a texture into slot (ref-counted).
func (m *RawMaterial) SetTexture(slot int, t Texture) { m.store.setTexture(m.ref.id, slot, t) }

// Ref returns another generic Material handle to the same instance (for meshes).
func (m *RawMaterial) Ref() Material { return m.genericMaterial.Copy() }
