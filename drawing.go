package pix

import "github.com/bluescreen10/pix/glm"

type drawing struct {
	instanceId    uint32
	instanceCount uint32 // 1 for regular meshes, N for instanced meshes
	geo           *GeometryData
	mat           *MaterialData
	model         glm.Mat4f
	modelInv      glm.Mat4f
	bounds        Sphere

	// non-nil for instanced drawings; instWorldTransform is the node's world matrix
	instMatrices       []glm.Mat4f
	instWorldTransform glm.Mat4f
}
