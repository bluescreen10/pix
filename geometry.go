package pix

type GeometryFlags uint32

const (
	UsePosFlag = GeometryFlags(1 << iota)
	UseUVsFlag
	UseNormal
	UseInstancesFlag // set per-drawing for instanced meshes; not stored on GeometryConfig
	UseSkinningFlag  // geometry has JOINTS_0 and WEIGHTS_0 vertex attributes
	StaticFlag       // geometry is immutable; SetAttribute panics
)

// ShadowGeometryMask is the subset of geometry flags that affect shadow pipelines.
const ShadowGeometryMask = UsePosFlag | UseSkinningFlag

var attrNameToFlag = map[string]GeometryFlags{
	PositionAttrName:   UsePosFlag,
	UVAttrName:         UseUVsFlag,
	NormalAttrName:     UseNormal,
	SkinIndexAttrName:  UseSkinningFlag,
	SkinWeightAttrName: UseSkinningFlag,
}

// geometryFlagNames maps a flag bit index to its shader define. StaticFlag has no
// define (it's a CPU-side hint), so it is intentionally absent.
var geometryFlagNames = map[int]string{
	0: "USE_POSITION",
	1: "USE_UV",
	2: "USE_NORMAL",
	3: "USE_INSTANCES",
	4: "USE_SKINNING",
}

// GeometryConfig is the input description for a geometry: a set of named vertex
// attributes plus an optional index buffer. It is consumed by
// GeometrySystem.Create, which packs it into the shared attribute streams. The
// zero value is an empty config; build it fluently with the setters.
type GeometryConfig struct {
	attrs   []*Attribute
	indices []uint32
	flags   GeometryFlags
}

// AddAttribute appends a named vertex attribute (position, uv, normal, ...) and
// records its presence flag.
func (c *GeometryConfig) AddAttribute(attr *Attribute) *GeometryConfig {
	c.flags |= attrNameToFlag[attr.name]
	c.attrs = append(c.attrs, attr)
	return c
}

// SetIndices sets the (optional) index buffer. When absent, Create generates a
// trivial 0..n-1 index list.
func (c *GeometryConfig) SetIndices(indices []uint32) *GeometryConfig {
	c.indices = indices
	return c
}

// Static marks the geometry immutable: SetAttribute will panic on the resulting
// Geometry. Use it as a hint for geometry that never changes after upload.
func (c *GeometryConfig) Static() *GeometryConfig {
	c.flags |= StaticFlag
	return c
}

// attribute returns the named attribute, or nil if absent.
func (c *GeometryConfig) attribute(name string) *Attribute {
	for _, a := range c.attrs {
		if a.name == name {
			return a
		}
	}
	return nil
}

// Geometry is the public handle for a renderer-owned geometry resource. It embeds
// a reference count, so it can be Copy()'d, Release()'d and garbage collected.
type Geometry struct {
	owner *GeometrySystem
	ref   Ref[Geometry]
}

func (g Geometry) Ref() Ref[Geometry] { return g.ref }

// Release surrenders this handle's reference to the geometry resource.
func (g Geometry) Release() { g.ref.Release() }

// Copy increments the reference count and returns an additional Geometry handle.
func (g Geometry) Copy() Geometry { return Geometry{owner: g.owner, ref: g.ref.Copy()} }

// Valid reports whether the underlying geometry resource is still alive.
func (g Geometry) Valid() bool { return g.ref.Valid() }

// BoundingSphere returns the geometry's local-space bounding sphere.
func (g Geometry) BoundingSphere() Sphere {
	return g.owner.Get(g.ref.ID()).bounds
}

// GetAttribute returns the raw bytes of a named vertex attribute.
func (g Geometry) GetAttribute(name string) []byte {
	return g.owner.GetAttribute(g.ref.ID(), name)
}

// SetAttribute replaces a named vertex attribute's data and re-uploads the
// affected stream. Panics if the geometry was created Static.
func (g Geometry) SetAttribute(name string, data []byte) {
	g.owner.SetAttribute(g.ref.ID(), name, data)
}
