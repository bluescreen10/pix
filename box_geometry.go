package pix

import "github.com/bluescreen10/pix/glm"

func NewBoxGeometry(width, height, depth float32) *GeometryData {
	hw, hh, hd := width/2, height/2, depth/2

	pos := []glm.Vec3f{
		// Left face (-X)
		{-hw, -hh, -hd}, // 0
		{-hw, -hh, hd},  // 1
		{-hw, hh, hd},   // 2
		{-hw, hh, -hd},  // 3
		// Right face (+X)
		{hw, -hh, -hd}, // 4
		{hw, -hh, hd},  // 5
		{hw, hh, hd},   // 6
		{hw, hh, -hd},  // 7
		// Bottom face (-Y)
		{-hw, -hh, -hd}, // 8
		{hw, -hh, -hd},  // 9
		{hw, -hh, hd},   // 10
		{-hw, -hh, hd},  // 11
		// Top face (+Y)
		{-hw, hh, -hd}, // 12
		{-hw, hh, hd},  // 13
		{hw, hh, hd},   // 14
		{hw, hh, -hd},  // 15
		// Back face (-Z)
		{-hw, -hh, -hd}, // 16
		{-hw, hh, -hd},  // 17
		{hw, hh, -hd},   // 18
		{hw, -hh, -hd},  // 19
		// Front face (+Z)
		{-hw, -hh, hd}, // 20
		{hw, -hh, hd},  // 21
		{hw, hh, hd},   // 22
		{-hw, hh, hd},  // 23
	}

	uvs := []glm.Vec2f{
		// Left face (-X)
		{0, 0}, {1, 0}, {1, 1}, {0, 1},
		// Right face (+X)
		{0, 0}, {1, 0}, {1, 1}, {0, 1},
		// Bottom face (-Y)
		{0, 0}, {1, 0}, {1, 1}, {0, 1},
		// Top face (+Y)
		{0, 0}, {0, 1}, {1, 1}, {1, 0},
		// Back face (-Z)
		{0, 0}, {0, 1}, {1, 1}, {1, 0},
		// Front face (+Z)
		{0, 0}, {1, 0}, {1, 1}, {0, 1},
	}

	indices := []uint32{
		// Left face (-X)
		0, 1, 2,
		0, 2, 3,

		// Right face (+X)
		4, 6, 5,
		4, 7, 6,

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
	}

	//FIXME: provide a constructor
	return (&GeometryData{}).
		AddAttribute(NewAttribute(PositionAttrName, PositionLocation, Float32x3, pos)).
		AddAttribute(NewAttribute(UVAttrName, UVLocation, Float32x2, uvs)).
		SetIndices(indices)
}
