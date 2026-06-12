package pix

type EquirectMaterial struct {
	Material
}

func (m *EquirectMaterial) data() *MaterialData {
	return m.renderer.materials.get(m.ref.ID())
}

func (m *EquirectMaterial) Release() {
	m.Material.Release()
	m.Material = Material{}
}

func (r *Renderer) NewEquirectMaterial() *EquirectMaterial {
	mat := NewMaterial("Equirect", "equirect_vertex.wesl", "equirect_fragment.wesl", []*Uniform{}, 0, false)
	mat.side = SideBack
	mat.depthFunc = DepthFuncLessEqual
	mat.colorWrite = true
	return &EquirectMaterial{Material: r.allocMaterialSlot(mat)}
}
