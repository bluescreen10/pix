package pix

import (
	"github.com/bluescreen10/pix/glm"
	"github.com/chewxy/math32"
)

func NewSphereGeometry(radius float32, widthSegments, heightSegments int) *GeometryData {

	vertexCount := (widthSegments + 1) * (heightSegments + 1)

	pos := make([]glm.Vec3f, 0, vertexCount)
	normals := make([]glm.Vec3f, 0, vertexCount)
	uvs := make([]glm.Vec2f, 0, vertexCount)

	indexCount := widthSegments * heightSegments * 6
	indices := make([]uint32, 0, indexCount)

	// Generate vertices
	for iy := 0; iy <= heightSegments; iy++ {

		v := float32(iy) / float32(heightSegments)
		theta := v * math32.Pi

		sinTheta, cosTheta := math32.Sincos(theta)

		for ix := 0; ix <= widthSegments; ix++ {

			u := float32(ix) / float32(widthSegments)
			phi := u * 2 * math32.Pi

			sinPhi, cosPhi := math32.Sincos(phi)

			x := -radius * cosPhi * sinTheta
			y := radius * cosTheta
			z := radius * sinPhi * sinTheta

			pos = append(pos, glm.Vec3f{x, y, z})
			normals = append(normals, glm.Vec3f{
				x / radius,
				y / radius,
				z / radius,
			})

			uvs = append(uvs, glm.Vec2f{
				u,
				1 - v,
			})
		}
	}

	// Generate indices
	rowSize := widthSegments + 1

	for iy := 0; iy < heightSegments; iy++ {
		for ix := 0; ix < widthSegments; ix++ {

			a := uint32(iy*rowSize + ix)
			b := a + 1
			c := uint32((iy+1)*rowSize + ix)
			d := c + 1

			indices = append(indices,
				a, c, b,
				c, d, b,
			)
		}
	}

	minY := float32(999999)
	maxY := float32(-999999)

	for _, p := range pos {
		minY = min(minY, p[1])
		maxY = max(maxY, p[1])
	}

	return (&GeometryData{}).
		AddAttribute(NewAttribute(PositionAttrName, PositionLocation, Float32x3, pos)).
		AddAttribute(NewAttribute(UVAttrName, UVLocation, Float32x2, uvs)).
		AddAttribute(NewAttribute(NormalAttrName, NormalLocation, Float32x3, normals)).
		SetIndices(indices)
}
