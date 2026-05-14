package pix

import (
	"github.com/bluescreen10/dawn-go/wgpu"
	"github.com/bluescreen10/pix/glm"
)

// ShadowMaterial holds the per-light view-projection uniform used during the
// shadow pass. It goes through the same prepareMaterial path as BasicMaterial.
type ShadowMaterial struct {
	Material
}

func (m *ShadowMaterial) data() *MaterialData {
	return m.renderer.materials.get(m.ref.ID())
}

func (m *ShadowMaterial) SetViewProjection(vp glm.Mat4f) {
	data := m.data()
	data.uniforms[0].SetMat4("viewProjection", vp)
	data.version++
}

func (m *ShadowMaterial) BindGroup() *wgpu.BindGroup {
	return m.data().gpuBindGroup
}

func (m *ShadowMaterial) BindGroupLayout() *wgpu.BindGroupLayout {
	return m.data().gpuBindGroupLayout
}

func (m *ShadowMaterial) Ref() Ref[Material] {
	return m.Material.ref
}

func (m *ShadowMaterial) Release() {
	m.Material.Release()
	m.Material = Material{}
}

func (r *Renderer) NewShadowMaterial() *ShadowMaterial {
	uniform := (&Uniform{}).AddMat4("viewProjection").Build()
	data := NewMaterial("Shadow", "shadow.wgsl", []*Uniform{uniform}, 0, false)
	data.side = SideBack // cull front faces to reduce shadow acne
	data.depthFunc = DepthFuncLessEqual
	data.colorWrite = false
	return &ShadowMaterial{Material: r.allocMaterialSlot(data)}
}
