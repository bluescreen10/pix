package pix

import "github.com/bluescreen10/pix/glm"

func NewBoxGeometry(width, height, depth float32) *Geometry {
	return &Geometry{
		isDirty: true,
		positions: []glm.Vec3f{
			{-width, -height, -depth},
			{-width, -height, depth},
			{-width, height, depth},
			{-width, height, -depth},
			{width, -height, -depth},
			{width, -height, depth},
			{width, height, -depth},
			{width, height, depth},
		},

		indices: []uint32{
			// Left face (-X)
			0, 1, 2,
			0, 2, 3,

			// Right face (+X)
			4, 6, 7,
			4, 7, 5,

			// Bottom face (-Y)
			0, 4, 5,
			0, 5, 1,

			// Top face (+Y)
			3, 2, 7,
			3, 7, 6,

			// Back face (-Z)
			0, 3, 6,
			0, 6, 4,

			// Front face (+Z)
			1, 5, 7,
			1, 7, 2,
		},
	}
}
