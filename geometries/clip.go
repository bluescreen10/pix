package geometries

import (
	"github.com/bluescreen10/pix/glm"
	"github.com/chewxy/math32"
)

// clipNormalCutoff rejects source triangles whose facing deviates too far from
// the projection axis (+Z in clip space). cos(75°) is generous enough to wrap
// a rounded corner while still dropping a surface the projection only grazes.
const clipNormalCutoff = 0.2588

// clipOffsetFraction scales the outward push applied to emitted vertices relative
// to the projection box's depth, preventing the patch from z-fighting with the
// source surface.
const clipOffsetFraction = 0.001

// clipVertex is one polygon corner during clipping. Both fields are in clip space
// and are interpolated when a clip plane splits an edge.
type clipVertex struct {
	position glm.Vec3f
	normal   glm.Vec3f
}

func (vertex clipVertex) lerp(other clipVertex, amount float32) clipVertex {
	return clipVertex{
		position: vertex.position.Add(other.position.Sub(vertex.position).Scale(amount)),
		normal:   vertex.normal.Add(other.normal.Sub(vertex.normal).Scale(amount)),
	}
}

// Clip clips the geometry against a projection box and creates the resulting patch
// in the same Store. Position, orientation, and size use the geometry's local space,
// and the returned geometry uses that same space. The texture coordinates project
// along the box's local -Z axis.
//
// Clip returns the zero Geometry when the box misses the source or every candidate
// triangle faces away.
func (g Geometry) Clip(position glm.Vec3f, orientation glm.Quatf, size glm.Vec3f) Geometry {
	if !validClipSize(size) {
		panic("geometries: Geometry.Clip requires finite, positive size components")
	}

	sourcePositions := g.AttributeData[glm.Vec3f](AttributePosition)
	sourceIndices := g.Indices()
	if len(sourcePositions) == 0 || len(sourceIndices) < 3 {
		return Geometry{}
	}
	sourceNormals := g.AttributeData[glm.Vec3f](AttributeNormal)

	// Work in clip space, where the projection box is axis-aligned and centered on
	// the origin. The transform has unit scale so size remains the explicit extent
	// and UV divisor.
	clipToGeometry := glm.Transform(glm.Vec3f{1, 1, 1}, orientation, position)
	geometryToClip := clipToGeometry.Inv()
	normalToClip := clipToGeometry.Mat3().Transpose()
	normalToGeometry := geometryToClip.Mat3().Transpose()
	half := glm.Vec3f{size[0] / 2, size[1] / 2, size[2] / 2}

	var (
		positions []glm.Vec3f
		normals   []glm.Vec3f
		uvs       []glm.Vec2f
		indices   []uint32
		polygon   []clipVertex
		scratch   []clipVertex
	)
	offset := size[2] * clipOffsetFraction

	for i := 0; i+2 < len(sourceIndices); i += 3 {
		index0 := sourceIndices[i]
		index1 := sourceIndices[i+1]
		index2 := sourceIndices[i+2]
		if int(index0) >= len(sourcePositions) || int(index1) >= len(sourcePositions) || int(index2) >= len(sourcePositions) {
			continue
		}

		position0 := transformClipPoint(geometryToClip, sourcePositions[index0])
		position1 := transformClipPoint(geometryToClip, sourcePositions[index1])
		position2 := transformClipPoint(geometryToClip, sourcePositions[index2])
		if outsideClipBox(position0, position1, position2, half) {
			continue
		}

		faceNormal := position1.Sub(position0).Cross(position2.Sub(position0))
		if faceNormal.Length() == 0 {
			continue
		}
		normal0 := transformClipNormal(normalToClip, sourceNormals, index0, faceNormal)
		normal1 := transformClipNormal(normalToClip, sourceNormals, index1, faceNormal)
		normal2 := transformClipNormal(normalToClip, sourceNormals, index2, faceNormal)

		// Authored normals point outward independently of source winding, so their
		// average provides a stable facing test against the +Z projection axis.
		facing := normal0.Add(normal1).Add(normal2)
		if length := facing.Length(); length == 0 || facing.Scale(1 / length)[2] < clipNormalCutoff {
			continue
		}

		polygon = polygon[:0]
		polygon = append(polygon,
			clipVertex{position: position0, normal: normal0},
			clipVertex{position: position1, normal: normal1},
			clipVertex{position: position2, normal: normal2},
		)
		polygon = clipPolygonToBox(polygon, &scratch, half)
		if len(polygon) < 3 {
			continue
		}

		base := uint32(len(positions))
		for _, vertex := range polygon {
			normal := vertex.normal
			if length := normal.Length(); length > 0 {
				normal = normal.Scale(1 / length)
			} else {
				normal = glm.Vec3f{0, 0, 1}
			}

			// Push outward in clip space so the offset follows the surface rather than
			// the projection direction.
			position := vertex.position.Add(normal.Scale(offset))
			positions = append(positions, transformClipPoint(clipToGeometry, position))
			normals = append(normals, normalToGeometry.MulVec3(normal).Normalize())
			uvs = append(uvs, glm.Vec2f{
				vertex.position[0]/size[0] + 0.5,
				vertex.position[1]/size[1] + 0.5,
			})
		}
		for index := 1; index+1 < len(polygon); index++ {
			indices = append(indices, base, base+uint32(index), base+uint32(index)+1)
		}
	}

	if len(positions) == 0 {
		return Geometry{}
	}
	return g.store.Create(GeometryConfig{
		Attributes: []Attribute{
			NewAttribute(AttributePosition, Float32x3, positions),
			NewAttribute(AttributeNormal, Float32x3, normals),
			NewAttribute(AttributeUV, Float32x2, uvs),
		},
		Indices: indices,
	})
}

// clipPolygonToBox clips a convex polygon against the box's six half-spaces using
// Sutherland-Hodgman clipping. scratch alternates with the input buffer to avoid an
// allocation for every source triangle.
func clipPolygonToBox(polygon []clipVertex, scratch *[]clipVertex, half glm.Vec3f) []clipVertex {
	for axis := range 3 {
		if len(polygon) < 3 {
			break
		}
		for _, positive := range [2]bool{true, false} {
			limit := half[axis]
			distance := func(vertex clipVertex) float32 {
				if positive {
					return limit - vertex.position[axis]
				}
				return vertex.position[axis] + limit
			}

			output := (*scratch)[:0]
			for i := range polygon {
				current := polygon[i]
				previous := polygon[(i+len(polygon)-1)%len(polygon)]
				currentDistance := distance(current)
				previousDistance := distance(previous)
				if currentDistance >= 0 {
					if previousDistance < 0 {
						amount := previousDistance / (previousDistance - currentDistance)
						output = append(output, previous.lerp(current, amount))
					}
					output = append(output, current)
				} else if previousDistance >= 0 {
					amount := previousDistance / (previousDistance - currentDistance)
					output = append(output, previous.lerp(current, amount))
				}
			}
			polygon, *scratch = output, polygon
			if len(polygon) < 3 {
				return polygon[:0]
			}
		}
	}
	return polygon
}

func outsideClipBox(position0, position1, position2, half glm.Vec3f) bool {
	for axis := range 3 {
		if position0[axis] > half[axis] && position1[axis] > half[axis] && position2[axis] > half[axis] {
			return true
		}
		if position0[axis] < -half[axis] && position1[axis] < -half[axis] && position2[axis] < -half[axis] {
			return true
		}
	}
	return false
}

func transformClipPoint(matrix glm.Mat4f, position glm.Vec3f) glm.Vec3f {
	result := matrix.Mul4x1(glm.Vec4f{position[0], position[1], position[2], 1})
	return glm.Vec3f{result[0], result[1], result[2]}
}

func transformClipNormal(matrix glm.Mat3[float32], normals []glm.Vec3f, index uint32, faceNormal glm.Vec3f) glm.Vec3f {
	if int(index) < len(normals) {
		return matrix.MulVec3(normals[index])
	}
	return faceNormal
}

func validClipSize(size glm.Vec3f) bool {
	for _, component := range size {
		if component <= 0 || math32.IsNaN(component) || math32.IsInf(component, 0) {
			return false
		}
	}
	return true
}
