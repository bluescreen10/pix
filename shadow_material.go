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
	data := NewMaterial("Shadow", "shadow_vertex.wgsl", "", []*Uniform{uniform}, 0, false)
	data.side = SideBack // cull front faces to reduce shadow acne
	data.depthFunc = DepthFuncLessEqual
	data.colorWrite = false
	return &ShadowMaterial{Material: r.allocMaterialSlot(data)}
}

// PointShadowMaterial is used for the 6-face cube shadow pass of point lights.
// It has a fragment stage that writes linear depth (dist/far) to @builtin(frag_depth).
type PointShadowMaterial struct {
	Material
}

func (m *PointShadowMaterial) data() *MaterialData {
	return m.renderer.materials.get(m.ref.ID())
}

func (m *PointShadowMaterial) SetFaceUniforms(vp glm.Mat4f, lightPos glm.Vec3f, far float32) {
	data := m.data()
	data.uniforms[0].SetMat4("viewProjection", vp)
	data.uniforms[0].SetVec4("lightPosAndFar", glm.Vec4f{lightPos[0], lightPos[1], lightPos[2], far})
	data.version++
}

func (m *PointShadowMaterial) BindGroup() *wgpu.BindGroup {
	return m.data().gpuBindGroup
}

func (m *PointShadowMaterial) BindGroupLayout() *wgpu.BindGroupLayout {
	return m.data().gpuBindGroupLayout
}

func (m *PointShadowMaterial) Ref() Ref[Material] {
	return m.Material.ref
}

func (m *PointShadowMaterial) Release() {
	m.Material.Release()
	m.Material = Material{}
}

func (r *Renderer) NewPointShadowMaterial() *PointShadowMaterial {
	// Single uniform buffer: mat4 viewProjection + vec4 lightPosAndFar = 80 bytes
	uniform := (&Uniform{}).AddMat4("viewProjection").AddVec4("lightPosAndFar").Build()
	data := NewMaterial("PointShadow", "point_shadow_vertex.wgsl", "point_shadow_fragment.wgsl", []*Uniform{uniform}, 0, false)
	// No face culling: normal-offset bias in the main shader handles acne.
	// The Y-flip applied to the projection matrix would invert winding, so culling
	// by face would need to be inverted too — avoiding it entirely is simpler.
	data.side = SideBoth
	data.depthFunc = DepthFuncLessEqual
	data.colorWrite = false
	return &PointShadowMaterial{Material: r.allocMaterialSlot(data)}
}
