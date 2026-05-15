package pix

import (
	"github.com/bluescreen10/dawn-go/wgpu"
)

func createVertexLayout(data *GeometryData) []wgpu.VertexBufferLayout {
	return createVertexLayoutMasked(data, ^GeometryFlags(0))
}

func createShadowVertexLayout(data *GeometryData) []wgpu.VertexBufferLayout {
	return createVertexLayoutMasked(data, ShadowGeometryMask)
}

func createVertexLayoutMasked(data *GeometryData, mask GeometryFlags) []wgpu.VertexBufferLayout {
	var layout []wgpu.VertexBufferLayout

	for _, a := range data.attrs {
		if attrNameToFlag[a.name]&mask == 0 {
			continue
		}
		layout = append(layout, wgpu.VertexBufferLayout{
			ArrayStride: uint64(a.Size()),
			StepMode:    wgpu.VertexStepModeVertex,
			Attributes: []wgpu.VertexAttribute{
				{
					Format:         attributeTypeFor[a.typ],
					Offset:         0,
					ShaderLocation: uint32(a.loc),
				},
			},
		})
	}

	return layout
}
