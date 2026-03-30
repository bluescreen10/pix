package pix

import (
	"fmt"
	"math"

	"github.com/bluescreen10/pix/glm"
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

	isBoundingSphereValid bool
	boundingSpehere       Sphere
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

		// if they set positions invlidate bounds
		if a.name == PositionAttrName {
			g.isBoundingSphereValid = false
		}
	}
	panic(fmt.Sprintf("geometry: attribute %q not found", name))
}

func (g *GeometryData) BoundingSphere() Sphere {
	if !g.isBoundingSphereValid {
		g.isBoundingSphereValid = true
		g.calcBoundingSphere()
	}

	return g.boundingSpehere
}

func (g *GeometryData) calcBoundingSphere() {
	points := CastTo[glm.Vec3f](g.AttributeData(PositionAttrName))

	g.boundingSpehere.Center = glm.Vec3f{0, 0, 0}
	g.boundingSpehere.Radius = 0

	if len(points) == 0 {
		return
	}

	// center
	for _, p := range points {
		g.boundingSpehere.Center[0] += p[0]
		g.boundingSpehere.Center[1] += p[1]
		g.boundingSpehere.Center[2] += p[2]
	}

	inv := 1.0 / float32(len(points))
	g.boundingSpehere.Center[0] *= inv
	g.boundingSpehere.Center[1] *= inv
	g.boundingSpehere.Center[2] *= inv

	//radius
	var maxDistSq float32
	for _, p := range points {
		d := p.Sub(g.boundingSpehere.Center)
		dSq := d[0]*d[0] + d[1]*d[1] + d[2]*d[2]

		if dSq > maxDistSq {
			maxDistSq = dSq
		}
	}

	g.boundingSpehere.Radius = float32(math.Sqrt(float64(maxDistSq)))
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
