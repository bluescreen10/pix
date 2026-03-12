package pix

import (
	"fmt"

	"github.com/cogentcore/webgpu/wgpu"
)

type GeometryData struct {
	version int
	slot    int
	indices []uint32
	attrs   []*Attribute
}

func (g *GeometryData) Indices() []uint32 {
	return g.indices
}

func (g *GeometryData) SetIndices(indices []uint32) {
	g.indices = indices
	g.version++
}

func (g *GeometryData) AttributeData(name string) []byte {
	for _, a := range g.attrs {
		if a.name == name {
			return a.data
		}
	}
	panic(fmt.Sprintf("geometry: attribute %q not found", name))
}

func (g *GeometryData) SetAttributeData(name string, data []byte) {
	for _, a := range g.attrs {
		if a.name == name {
			a.SetBytes(data)
		}
	}
	panic(fmt.Sprintf("geometry: attribute %q not found", name))
}

type Geometry struct {
	version int
	index   *wgpu.Buffer
	bufs    []GeometryBuffer
	count   int
	layout  []wgpu.VertexBufferLayout
}

type GeometryBuffer struct {
	version int
	loc     int
	buf     *wgpu.Buffer
}

func (g Geometry) Destroy() {
	if g.index != nil {
		g.index.Destroy()
	}

	for _, gb := range g.bufs {
		if gb.buf != nil {
			gb.buf.Destroy()
		}
	}
}
