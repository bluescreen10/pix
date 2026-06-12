package pix

import "github.com/bluescreen10/pix/glm"

func (r *Renderer) NewHalfQuad() Geometry {
	pos := []glm.Vec3f{
		{-1, -1, 0.999},
		{-1, 3, 0.999},
		{3, -1, 0.999},
	}
	return r.allocGeometrySlot((&GeometryData{}).AddAttribute(NewAttribute(PositionAttrName, PositionLocation, Float32x3, pos)))
}

func (r *Renderer) NewQuad() Geometry {
	pos := []glm.Vec3f{
		{-1, -1, 0.999},
		{-1, 1, 0.999},
		{1, -1, 0.999},
		{-1, 1, 0.999},
		{1, -1, 0.999},
		{-1, -1, 0.999},
	}
	return r.allocGeometrySlot((&GeometryData{}).AddAttribute(NewAttribute(PositionAttrName, PositionLocation, Float32x3, pos)))
}
