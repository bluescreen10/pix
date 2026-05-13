package pix

import "github.com/bluescreen10/pix/glm"

type drawing struct {
	instanceId uint32
	geo        *GeometryData
	mat        *MaterialData
	model      glm.Mat4f
	modelInv   glm.Mat4f
	bounds     Sphere
}
