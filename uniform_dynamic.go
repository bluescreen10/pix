package pix

import (
	"encoding/binary"
	"fmt"
	"math"

	"github.com/bluescreen10/pix/glm"
)

type UniformType int

const (
	FloatUniformType = iota
	Vec2UniformType
	Vec3UniformType
	Vec4UniformType
	UintUniformType
	BoolUniformType
	StructUniformType
)

var uniformTypeSize = map[UniformType]int{
	FloatUniformType:  4,
	Vec2UniformType:   8,
	Vec3UniformType:   12,
	Vec4UniformType:   16,
	UintUniformType:   4,
	BoolUniformType:   4,
	StructUniformType: 0,
}

var typeNames = map[UniformType]string{
	FloatUniformType:  "float",
	Vec2UniformType:   "vec2",
	Vec3UniformType:   "vec3",
	Vec4UniformType:   "vec4",
	UintUniformType:   "uint",
	BoolUniformType:   "bool",
	StructUniformType: "struct",
}

type uniformField struct {
	name       string
	typ        UniformType
	offset     int
	structData *Uniform
}

type Uniform struct {
	fields []uniformField
	size   int
	data   []byte

	dataOffset int
}

func (u *Uniform) AddFloat(name string) *Uniform {
	return u.addField(name, FloatUniformType, nil)
}

func (u *Uniform) AddUint(name string) *Uniform {
	return u.addField(name, UintUniformType, nil)
}

func (u *Uniform) AddBool(name string) *Uniform {
	return u.addField(name, BoolUniformType, nil)
}

func (u *Uniform) AddVec2(name string) *Uniform {
	return u.addField(name, Vec2UniformType, nil)
}

func (u *Uniform) AddVec3(name string) *Uniform {
	return u.addField(name, Vec3UniformType, nil)
}

func (u *Uniform) AddVec4(name string) *Uniform {
	return u.addField(name, Vec4UniformType, nil)
}

func (u *Uniform) AddStruct(name string, nested *Uniform) *Uniform {
	return u.addField(name, StructUniformType, nested)
}

func (u *Uniform) Build() *Uniform {
	u.size = align16(u.size)
	u.data = make([]byte, u.size)
	u.wireStructs()
	return u
}

func (u *Uniform) Bytes() []byte {
	return u.data
}

func (u *Uniform) Float(name string) float32 {
	f := u.mustField(name, FloatUniformType)
	v := binary.LittleEndian.Uint32(u.bufAt(f.offset))
	return math.Float32frombits(v)
}

func (u *Uniform) SetFloat(name string, v float32) {
	f := u.mustField(name, FloatUniformType)
	binary.LittleEndian.PutUint32(u.bufAt(f.offset), math.Float32bits(v))
}

func (u *Uniform) Vec2(name string) glm.Vec2f {
	f := u.mustField(name, Vec2UniformType)
	b := u.bufAt(f.offset)
	v0 := math.Float32frombits(binary.LittleEndian.Uint32(b[0:]))
	v1 := math.Float32frombits(binary.LittleEndian.Uint32(b[4:]))
	return glm.Vec2f{v0, v1}
}

func (u *Uniform) SetVec2(name string, v glm.Vec2f) {
	f := u.mustField(name, Vec2UniformType)
	b := u.bufAt(f.offset)
	binary.LittleEndian.PutUint32(b[0:], math.Float32bits(v[0]))
	binary.LittleEndian.PutUint32(b[4:], math.Float32bits(v[1]))
}

func (u *Uniform) Vec3(name string) glm.Vec3f {
	f := u.mustField(name, Vec3UniformType)
	b := u.bufAt(f.offset)
	v0 := math.Float32frombits(binary.LittleEndian.Uint32(b[0:]))
	v1 := math.Float32frombits(binary.LittleEndian.Uint32(b[4:]))
	v2 := math.Float32frombits(binary.LittleEndian.Uint32(b[8:]))
	return glm.Vec3f{v0, v1, v2}
}

func (u *Uniform) SetVec3(name string, v glm.Vec3f) {
	f := u.mustField(name, Vec3UniformType)
	b := u.bufAt(f.offset)
	binary.LittleEndian.PutUint32(b[0:], math.Float32bits(v[0]))
	binary.LittleEndian.PutUint32(b[4:], math.Float32bits(v[1]))
	binary.LittleEndian.PutUint32(b[8:], math.Float32bits(v[2]))
}

func (u *Uniform) Vec4(name string) glm.Vec4f {
	f := u.mustField(name, Vec4UniformType)
	b := u.bufAt(f.offset)
	v0 := math.Float32frombits(binary.LittleEndian.Uint32(b[0:]))
	v1 := math.Float32frombits(binary.LittleEndian.Uint32(b[4:]))
	v2 := math.Float32frombits(binary.LittleEndian.Uint32(b[8:]))
	v3 := math.Float32frombits(binary.LittleEndian.Uint32(b[12:]))
	return glm.Vec4f{v0, v1, v2, v3}
}

func (u *Uniform) SetVec4(name string, v glm.Vec4f) {
	f := u.mustField(name, Vec4UniformType)
	b := u.bufAt(f.offset)
	binary.LittleEndian.PutUint32(b[0:], math.Float32bits(v[0]))
	binary.LittleEndian.PutUint32(b[4:], math.Float32bits(v[1]))
	binary.LittleEndian.PutUint32(b[8:], math.Float32bits(v[2]))
	binary.LittleEndian.PutUint32(b[12:], math.Float32bits(v[3]))
}

func (u *Uniform) Struct(name string) *Uniform {
	return u.mustField(name, StructUniformType).structData
}

func (u *Uniform) Size() int {
	return u.size
}

func (u *Uniform) addField(name string, typ UniformType, nested *Uniform) *Uniform {
	if u.data != nil {
		panic(fmt.Sprintf("uniform: cannot add field %s after buffer has been initialized", name))
	}

	offset := align16(u.size)
	fieldSize := uniformTypeSize[typ]

	if typ == StructUniformType {
		if nested == nil {
			panic(fmt.Sprintf("uniform: struct(%s) cannot be nil", name))
		}
		fieldSize = align16(nested.size)
	}

	u.fields = append(u.fields, uniformField{
		name:       name,
		typ:        typ,
		offset:     offset,
		structData: nested,
	})
	u.size = offset + fieldSize
	return u
}

func (u *Uniform) bufAt(offset int) []byte { return u.data[offset:] }

func (u *Uniform) mustField(name string, typ UniformType) uniformField {
	f, ok := u.findField(name)
	if !ok {
		panic(fmt.Sprintf("uniform: field %s does not exists", name))
	}

	if f.typ != typ {
		panic(fmt.Sprintf("uniform: field %s type mismatch, want %s got %s", name, typeNames[typ], typeNames[f.typ]))
	}

	return f
}

func (u *Uniform) findField(name string) (uniformField, bool) {
	for _, f := range u.fields {
		if f.name == name {
			return f, true
		}
	}
	return uniformField{}, false
}

func (u *Uniform) wireStructs() {
	for _, f := range u.fields {
		if f.typ != StructUniformType {
			continue
		}

		nested := f.structData
		nested.dataOffset = u.dataOffset + f.offset
		nested.data = u.data[nested.dataOffset : nested.dataOffset+nested.size]
		nested.wireStructs()
	}
}

func align16(x int) int { return ((x + 15) / 16) * 16 }
