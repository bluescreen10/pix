package pix

import "github.com/bluescreen10/pix/glm"

func NewBoxGeometry(width, height, depth float32) *Geometry {
	hw, hh, hd := width/2, height/2, depth/2
	return &Geometry{
		isDirty: true,
		positions: []glm.Vec3f{
			// Left face (-X)
			{-hw, -hh, -hd},
			{-hw, -hh, hd},
			{-hw, hh, hd},
			{-hw, hh, -hd},
			// Right face (+X)
			{hw, -hh, -hd},
			{hw, -hh, hd},
			{hw, hh, -hd},
			{hw, hh, hd},
			// Bottom face (-Y)
			{-hw, -hh, -hd},
			{hw, -hh, -hd},
			{hw, -hh, hd},
			{-hw, -hh, hd},
			// Top face (+Y)
			{-hw, hh, -hd},
			{-hw, hh, hd},
			{hw, hh, hd},
			{hw, hh, -hd},
			// Back face (-Z)
			{-hw, -hh, -hd},
			{-hw, hh, -hd},
			{hw, hh, -hd},
			{hw, -hh, -hd},
			// Front face (+Z)
			{-hw, -hh, hd},
			{hw, -hh, hd},
			{hw, hh, hd},
			{-hw, hh, hd},
		},

		indices: []uint32{
			// Left face (-X)
			0, 1, 2,
			0, 2, 3,

			// Right face (+X)
			4, 6, 7,
			4, 7, 5,

			// Bottom face (-Y)
			8, 9, 10,
			8, 10, 11,

			// Top face (+Y)
			12, 13, 14,
			12, 14, 15,

			// Back face (-Z)
			16, 17, 18,
			16, 18, 19,

			// Front face (+Z)
			20, 21, 22,
			20, 22, 23,
		},

		uvs: []glm.Vec2f{
			// Left face (-X)
			{0, 0},
			{1, 0},
			{1, 1},
			{0, 1},
			// Right face (+X)
			{1, 0},
			{0, 0},
			{0, 1},
			{1, 1},
			// Bottom face (-Y)
			{0, 0},
			{1, 0},
			{1, 1},
			{0, 1},
			// Top face (+Y)
			{0, 1},
			{0, 0},
			{1, 0},
			{1, 1},
			// Back face (-Z)
			{1, 0},
			{1, 1},
			{0, 1},
			{0, 0},
			// Front face (+Z)
			{0, 0},
			{1, 0},
			{1, 1},
			{0, 1},
		},
	}
}
