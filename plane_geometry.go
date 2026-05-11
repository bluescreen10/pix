package pix

import "github.com/bluescreen10/pix/glm"

func NewPlaneGeometry(width, height float32, widthSegments, heightSegments int) *GeometryData {
	cols := widthSegments + 1
	rows := heightSegments + 1
	hw, hh := width/2, height/2

	pos := make([]glm.Vec3f, 0, cols*rows)
	uvs := make([]glm.Vec2f, 0, cols*rows)
	normals := make([]glm.Vec3f, 0, cols*rows)
	indices := make([]uint32, 0, widthSegments*heightSegments*6)

	for ix := 0; ix < cols; ix++ {
		u := float32(ix) / float32(widthSegments)
		x := -hw + u*width
		for iz := 0; iz < rows; iz++ {
			v := float32(iz) / float32(heightSegments)
			z := -hh + v*height
			pos = append(pos, glm.Vec3f{x, 0, z})
			uvs = append(uvs, glm.Vec2f{u, v})
			normals = append(normals, glm.Vec3f{0, 1, 0})
		}
	}

	for ix := 0; ix < widthSegments; ix++ {
		for iz := 0; iz < heightSegments; iz++ {
			a := uint32(ix*rows + iz)
			b := uint32(ix*rows + iz + 1)
			c := uint32((ix+1)*rows + iz + 1)
			d := uint32((ix+1)*rows + iz)
			indices = append(indices, a, b, c, a, c, d)
		}
	}

	return (&GeometryData{}).
		AddAttribute(NewAttribute(PositionAttrName, PositionLocation, Float32x3, pos)).
		AddAttribute(NewAttribute(UVAttrName, UVLocation, Float32x2, uvs)).
		AddAttribute(NewAttribute(NormalAttrName, NormalLocation, Float32x3, normals)).
		SetIndices(indices)
}
