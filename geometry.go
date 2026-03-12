package pix

import (
	"fmt"

	"github.com/cogentcore/webgpu/wgpu"
)

type GeometryFlags uint64

const (
	UsePosFlag = GeometryFlags(1 << iota)
	UseUVsFlag
	UseNormal
)

var attrNameToFlag = map[string]GeometryFlags{
	PositionAttrName: UsePosFlag,
	UVAttrName:       UseUVsFlag,
	NormalAttrName:   UseNormal,
}

var geometryFlagNames = map[int]string{
	0: "USE_POSITION",
	1: "USE_UV",
	2: "USE_NORMAL",
}

type GeometryData struct {
	version int
	slot    int
	indices []uint32
	attrs   []*Attribute
	flags   GeometryFlags
}

func (g *GeometryData) Indices() []uint32 {
	return g.indices
}

func (g *GeometryData) SetIndices(indices []uint32) *GeometryData {
	g.indices = indices
	g.version++
	return g
}

func (g *GeometryData) AddAttribute(attr *Attribute) *GeometryData {
	flag, _ := attrNameToFlag[attr.name]
	g.flags |= flag
	g.attrs = append(g.attrs, attr)
	g.version++
	return g
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
	topolgy wgpu.PrimitiveTopology
	version int
	index   *wgpu.Buffer
	bufs    []GeometryBuffer
	count   int
	layout  []wgpu.VertexBufferLayout
	flags   GeometryFlags
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
