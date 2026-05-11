package pix

import (
	"fmt"
	"unsafe"

	"github.com/bluescreen10/dawn-go/wgpu"
)

type AttributeType int

const (
	InvalidAttributeType = AttributeType(iota)
	Float32
	Float32x2
	Float32x3
	Float32x4

	Uint32
)

const (
	PositionLocation = iota
	UVLocation
	NormalLocation
	TangentLocation
	ColorLocation
)

const (
	PositionAttrName = "position"
	UVAttrName       = "uv"
	NormalAttrName   = "normal"
)

var attributeTypeFor = map[AttributeType]wgpu.VertexFormat{
	Float32:   wgpu.VertexFormatFloat32,
	Float32x2: wgpu.VertexFormatFloat32x2,
	Float32x3: wgpu.VertexFormatFloat32x3,
	Float32x4: wgpu.VertexFormatFloat32x4,
	Uint32:    wgpu.VertexFormatUint32,
}

func (t AttributeType) Size() int {
	switch t {
	case Float32, Uint32:
		return 4
	case Float32x2:
		return 8
	case Float32x3:
		return 12
	case Float32x4:
		return 16
	}
	panic("attribute: unkown attribute type")
}

type Attribute struct {
	version int
	name    string
	len     int
	loc     int //TODO maybe this can be inferred
	typ     AttributeType
	data    []byte
}

func (a *Attribute) Name() string {
	return a.name
}

func (a *Attribute) Bytes() []byte {
	return a.data
}

func (a *Attribute) SetBytes(data []byte) {
	expectedLen := a.typ.Size() * a.len
	if len(data) != expectedLen {
		panic(fmt.Sprintf("attribute %q: data length %d does not match reserved size %d", a.name, len(data), expectedLen))
	}

	a.version++
	a.data = data
}

func (a *Attribute) Size() int {
	return a.typ.Size()
}

func NewAttribute[T any](name string, loc int, t AttributeType, data []T) *Attribute {

	length := len(data)
	var zero T
	size := int(unsafe.Sizeof(zero))

	if size != t.Size() {
		panic(fmt.Sprintf("attribute %q: element size %d does not match %v size %d",
			name, size, t, t.Size()))
	}

	rawData := CastTo[byte, T](data)
	return &Attribute{version: 1, name: name, loc: loc, typ: t, len: length, data: rawData}
}

func CastTo[T, E any](data []E) []T {
	if len(data) == 0 {
		return nil
	}
	var zeroTo T
	var zeroFrom E
	fromBytes := len(data) * int(unsafe.Sizeof(zeroFrom))
	toLen := fromBytes / int(unsafe.Sizeof(zeroTo))
	ptr := unsafe.Pointer(&data[0])
	return unsafe.Slice((*T)(ptr), toLen)
}
