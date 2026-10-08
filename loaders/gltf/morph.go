package gltf

import (
	"encoding/binary"
	"math"

	"github.com/bluescreen10/pix/geometries"
	"github.com/bluescreen10/pix/glm"
)

// morphTargets reads a primitive's morph targets, named by names (which may be
// shorter than the target list, or nil). A target's deltas whose count does not
// match the primitive's vertex count are dropped rather than failing the load: the
// target still exists, so later targets keep their indices, it just moves nothing.
func (l *loader) morphTargets(prim primitive, vertexCount int, names []string) []geometries.MorphTarget {
	if len(prim.Targets) == 0 {
		return nil
	}
	targets := make([]geometries.MorphTarget, len(prim.Targets))
	for i, attributes := range prim.Targets {
		if i < len(names) {
			targets[i].Name = names[i]
		}
		for attribute, acc := range attributes {
			deltas := l.accessorVec3s(acc)
			if len(deltas) != vertexCount {
				continue
			}
			switch attribute {
			case "POSITION":
				targets[i].PositionDeltas = deltas
			case "NORMAL":
				targets[i].NormalDeltas = deltas
			case "TANGENT":
				targets[i].TangentDeltas = deltas
			}
		}
	}
	return targets
}

// targetNames returns the names of a mesh's morph targets: from the mesh's extras,
// where exporters usually put them, or else from the node's.
func targetNames(gm mesh, gn node) []string {
	if gm.Extras != nil && len(gm.Extras.TargetNames) > 0 {
		return gm.Extras.TargetNames
	}
	if gn.Extras != nil {
		return gn.Extras.TargetNames
	}
	return nil
}

// defaultMorphWeights returns the weights a node's mesh starts with: the node's own,
// if it has any, overriding the mesh's.
func defaultMorphWeights(gm mesh, gn node) []float32 {
	if len(gn.Weights) > 0 {
		return gn.Weights
	}
	return gm.Weights
}

// accessorVec3s reads a VEC3 accessor as float vectors, whatever its component type
// (see accessorFloats). Morph target deltas are often stored as quantized integers.
func (l *loader) accessorVec3s(idx int) []glm.Vec3f {
	if l.doc.Accessors[idx].Type != "VEC3" {
		return nil
	}
	floats := l.accessorFloats(idx)
	vectors := make([]glm.Vec3f, len(floats)/3)
	for i := range vectors {
		vectors[i] = glm.Vec3f{floats[i*3], floats[i*3+1], floats[i*3+2]}
	}
	return vectors
}

// accessorFloats reads every component of an accessor as a float. Integer components
// convert as glTF defines: a normalized one maps to [0,1] (unsigned) or [-1,1]
// (signed), any other keeps its integer value.
func (l *loader) accessorFloats(idx int) []float32 {
	acc := l.doc.Accessors[idx]
	raw := l.accessorBytes(idx)
	floats := make([]float32, acc.Count*typeComponents(acc.Type))
	for i := range floats {
		floats[i] = componentFloat(raw, i, acc.ComponentType, acc.Normalized)
	}
	return floats
}

// componentFloat reads component i of tightly packed components of componentType.
func componentFloat(raw []byte, i, componentType int, normalized bool) float32 {
	var value, scale float32
	switch componentType {
	case 5120: // BYTE
		value, scale = float32(int8(raw[i])), 127
	case 5121: // UNSIGNED_BYTE
		value, scale = float32(raw[i]), 255
	case 5122: // SHORT
		value, scale = float32(int16(binary.LittleEndian.Uint16(raw[i*2:]))), 32767
	case 5123: // UNSIGNED_SHORT
		value, scale = float32(binary.LittleEndian.Uint16(raw[i*2:])), 65535
	case 5125: // UNSIGNED_INT
		return float32(binary.LittleEndian.Uint32(raw[i*4:]))
	default: // 5126 FLOAT
		return math.Float32frombits(binary.LittleEndian.Uint32(raw[i*4:]))
	}
	if !normalized {
		return value
	}
	return max(value/scale, -1)
}
