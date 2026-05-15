package pix

import (
	"fmt"
	"math"

	"github.com/bluescreen10/dawn-go/wgpu"
	"github.com/bluescreen10/pix/glm"
)

type GeometryFlags uint32

const (
	UsePosFlag = GeometryFlags(1 << iota)
	UseUVsFlag
	UseNormal
)

// ShadowGeometryMask is the subset of geometry attributes consumed by the
// shadow shader. Only these slots are included in the shadow pipeline vertex
// layout, and only these flags contribute to the shadow pipeline cache key.
const ShadowGeometryMask = UsePosFlag

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
	// CPU-side mesh data.
	version int
	indices []uint32
	attrs   []*Attribute
	flags   GeometryFlags

	isBoundingSphereValid bool
	boundingSphere        Sphere

	// GPU-side resources, populated by the renderer.
	gpuVersion      int
	gpuIndex        *wgpu.Buffer
	gpuBufs         []GeometryBuffer
	gpuCount        int
	gpuLayout       []wgpu.VertexBufferLayout
	gpuShadowLayout []wgpu.VertexBufferLayout
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

	return g.boundingSphere
}

func (g *GeometryData) calcBoundingSphere() {
	points := CastTo[glm.Vec3f](g.AttributeData(PositionAttrName))

	g.boundingSphere.Center = glm.Vec3f{0, 0, 0}
	g.boundingSphere.Radius = 0

	if len(points) == 0 {
		return
	}

	for _, p := range points {
		g.boundingSphere.Center[0] += p[0]
		g.boundingSphere.Center[1] += p[1]
		g.boundingSphere.Center[2] += p[2]
	}

	inv := 1.0 / float32(len(points))
	g.boundingSphere.Center[0] *= inv
	g.boundingSphere.Center[1] *= inv
	g.boundingSphere.Center[2] *= inv

	var maxDistSq float32
	for _, p := range points {
		d := p.Sub(g.boundingSphere.Center)
		dSq := d[0]*d[0] + d[1]*d[1] + d[2]*d[2]
		if dSq > maxDistSq {
			maxDistSq = dSq
		}
	}

	g.boundingSphere.Radius = float32(math.Sqrt(float64(maxDistSq)))
}

// Destroy releases the GPU buffers held by this geometry.
func (g *GeometryData) Destroy() {
	if g.gpuIndex != nil {
		g.gpuIndex.Destroy()
		g.gpuIndex = nil
	}
	for _, gb := range g.gpuBufs {
		if gb.buf != nil {
			gb.buf.Destroy()
		}
	}
	g.gpuBufs = nil
}

type GeometryBuffer struct {
	version int
	loc     int
	buf     *wgpu.Buffer
}

// Geometry is the public handle for a renderer-owned geometry resource.
type Geometry struct {
	renderer *Renderer
	ref      Ref[Geometry]
}

func (g Geometry) Ref() Ref[Geometry] { return g.ref }

// Release surrenders this handle's reference to the geometry resource.
func (g Geometry) Release() { g.ref.Release() }

// Copy increments the reference count and returns an additional Geometry handle.
func (g Geometry) Copy() Geometry { return Geometry{renderer: g.renderer, ref: g.ref.Copy()} }

// Valid reports whether the underlying geometry resource is still alive.
func (g Geometry) Valid() bool { return g.ref.Valid() }

func (g Geometry) BoundingSphere() Sphere {
	return g.renderer.geometries.get(g.ref.ID()).BoundingSphere()
}
