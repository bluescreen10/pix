package pix

import (
	"github.com/cogentcore/webgpu/wgpu"
)

func createVertexLayout(data *GeometryData) []wgpu.VertexBufferLayout {
	var layout []wgpu.VertexBufferLayout

	for _, a := range data.attrs {
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
