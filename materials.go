package pix

import "github.com/bluescreen10/pix/materials"

// Shortcuts for the built-in material types on this renderer's MaterialStore.
//
// Geometry and textures need no equivalent: their store is the natural receiver, so
// r.GeometryStore.Create(cfg) already reads well. A material type has its own
// constructor per type, which pushes the store down into an argument —
// materials.NewPBRMaterial(r.MaterialStore) — and these hand that boilerplate back.
//
// They are shortcuts, not the API: a custom material type outside this package calls
// materials.New*/Pool.Create against r.MaterialStore directly, exactly as these do.

// NewBasicMaterial creates an unlit material with an unbound color map.
func (r *Renderer) NewBasicMaterial() *materials.BasicMaterial {
	return materials.NewBasicMaterial(r.MaterialStore)
}

// NewBlinnPhongMaterial creates a Blinn-Phong material with an unbound color map.
func (r *Renderer) NewBlinnPhongMaterial() *materials.BlinnPhongMaterial {
	return materials.NewBlinnPhongMaterial(r.MaterialStore)
}

// NewPBRMaterial creates a PBR material with four unbound maps: color, normal,
// metallic and roughness.
func (r *Renderer) NewPBRMaterial() *materials.PBRMaterial {
	return materials.NewPBRMaterial(r.MaterialStore)
}

// NewRawMaterial creates a self-describing material from your own shader and record
// size: the store allocates storage from dataSize, and you write the record bytes
// yourself (see materials.RawMaterial).
func (r *Renderer) NewRawMaterial(shader materials.Shader, dataSize, textureSlots int) *materials.RawMaterial {
	return materials.NewRawMaterial(r.MaterialStore, shader, dataSize, textureSlots)
}
