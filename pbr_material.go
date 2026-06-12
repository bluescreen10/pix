package pix

import (
	"github.com/bluescreen10/pix/glm"
)

type PBRMaterial struct {
	Material
}

func (m *PBRMaterial) data() *MaterialData {
	return m.renderer.materials.get(m.ref.ID())
}

func (m *PBRMaterial) Wireframe() bool { return m.data().flags&WireframeFlag != 0 }
func (m *PBRMaterial) SetWireframe(v bool) {
	d := m.data()
	if v {
		d.flags |= WireframeFlag
	} else {
		d.flags &^= WireframeFlag
	}
}

func (m *PBRMaterial) Color() glm.Color4f {
	v := m.data().uniforms[0].Vec4("color")
	return glm.Color4f{v[0], v[1], v[2], v[3]}
}

func (m *PBRMaterial) SetColor(color glm.Color4f) {
	data := m.data()
	data.uniforms[0].SetVec4("color", glm.Vec4f{color[0], color[1], color[2], color[3]})
	data.version++
}

func (m *PBRMaterial) ColorMap() Ref[Texture] {
	return m.data().textures[0]
}

func (m *PBRMaterial) SetColorMap(texture Texture) {
	data := m.data()
	old := data.textures[0]
	data.textures[0] = texture.ref.Copy()
	old.Release()
	if texture.ref.Valid() {
		data.flags |= ColorMapFlag
	} else {
		data.flags &^= ColorMapFlag
	}
	data.version++
}

func (m *PBRMaterial) Metallic() float32 {
	v := m.data().uniforms[0].Float("metallic")
	return v
}

func (m *PBRMaterial) SetMetallic(metallic float32) {
	data := m.data()
	data.uniforms[0].SetFloat("metallic", metallic)
	data.version++
}

func (m *PBRMaterial) Roughness() float32 {
	v := m.data().uniforms[0].Float("roughness")
	return v
}

func (m *PBRMaterial) SetRoughness(roughness float32) {
	data := m.data()
	data.uniforms[0].SetFloat("roughness", roughness)
	data.version++
}

func (m *PBRMaterial) AmbientOcclussion() float32 {
	v := m.data().uniforms[0].Float("ao")
	return v
}

func (m *PBRMaterial) SetAmbientOcclussion(ao float32) {
	data := m.data()
	data.uniforms[0].SetFloat("ao", ao)
	data.version++
}

func (m *PBRMaterial) NeedsUpdate() {
	m.data().version++
}

// Ref returns a new Material handle sharing the same underlying resource.
func (m *PBRMaterial) Ref() Material {
	return m.Material.Copy()
}

// Release surrenders the builder's own reference to the material resource.
func (m *PBRMaterial) Release() {
	m.Material.Release()
	m.Material = Material{}
}

// NewPBRMaterial creates a Blinn-Phong lit material owned by the renderer.
func (r *Renderer) NewPBRMaterial() *PBRMaterial {
	uniform := (&Uniform{}).AddVec4("color").AddFloat("metallic").AddFloat("roughness").AddFloat("ao").Build()
	data := NewMaterial("PBR Material", "vertex.wesl", "pbr_material.wesl", []*Uniform{uniform}, 1, true)
	mat := r.allocMaterialSlot(data)

	m := &PBRMaterial{Material: mat}
	m.SetColor(glm.Color4f{1, 1, 1, 1})
	m.SetMetallic(0)
	m.SetRoughness(0.5)
	m.SetAmbientOcclussion(1)
	return m
}
