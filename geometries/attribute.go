package geometries

// AttributeType is a vertex attribute's semantic role.
type AttributeType uint8

const (
	AttributePosition AttributeType = iota
	AttributeNormal
	AttributeColor
	AttributeUV
	AttributeSkinIndex  // skeleton-relative joint indices, per vertex (up to 4)
	AttributeSkinWeight // per-joint blend weights, per vertex (normalized at pack time)
	attributeCount
)

// DataType is an attribute element's CPU format. It records how NewAttribute's
// typed slice was laid out so GetAttributeData can hand it back in the same shape.
type DataType uint8

const (
	Float32 DataType = iota
	Float32x2
	Float32x3
	Float32x4
	Float64
	Int32
	Uint32
	Uint16x4
)

// canonicalDataType is the element format the fixed GPU packer expects for each
// known attribute; alloc rejects mismatches.
var canonicalDataType = [attributeCount]DataType{
	AttributePosition:   Float32x3,
	AttributeNormal:     Float32x3,
	AttributeColor:      Float32x4,
	AttributeUV:         Float32x2,
	AttributeSkinIndex:  Uint16x4,
	AttributeSkinWeight: Float32x4,
}

// Attribute is one named vertex stream, built with NewAttribute.
type Attribute struct {
	attrType AttributeType
	dataType DataType
	data     []byte // the typed slice reinterpreted as raw bytes (no copy)
	count    int    // element (vertex) count
}

// NewAttribute wraps a typed slice as an Attribute. dataType records the element
// layout (so GetAttributeData returns the same shape); data is reinterpreted as
// raw bytes without copying, so the backing array must outlive the geometry.
func NewAttribute[T any](attrType AttributeType, dataType DataType, data []T) Attribute {
	return Attribute{attrType: attrType, dataType: dataType, data: toBytes(data), count: len(data)}
}

// Type reports a's semantic role.
func (a Attribute) Type() AttributeType {
	return a.attrType
}

// AttributeData reinterprets a's element data as []T (matching the layout given to
// NewAttribute), before it has ever been uploaded — the pre-upload counterpart to
// Geometry.GetAttributeData. Do not mutate the result — it aliases a's internal bytes.
func AttributeData[T any](a Attribute) []T {
	return fromBytes[T](a.data, a.count)
}

// GeometryConfig is a geometry's source data: a set of vertex attributes (Position
// required, others optional and matching the position count), and an optional index
// list (nil → generated 0..n-1).
type GeometryConfig struct {
	Attributes []Attribute
	Indices    []uint32
}
