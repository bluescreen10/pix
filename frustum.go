package pix

import "github.com/bluescreen10/pix/glm"

type Frustum [6]glm.Vec4f

func NewFrustumFromViewProjection(viewProj glm.Mat4f) Frustum {

	return [6]glm.Vec4f{
		//left plane
		glm.Vec4f{
			viewProj[3] + viewProj[0],
			viewProj[7] + viewProj[4],
			viewProj[11] + viewProj[8],
			viewProj[15] + viewProj[12],
		}.Normalize(),

		//right plane
		glm.Vec4f{
			viewProj[3] - viewProj[0],
			viewProj[7] - viewProj[4],
			viewProj[11] - viewProj[8],
			viewProj[15] - viewProj[12],
		}.Normalize(),

		//top  plane
		glm.Vec4f{
			viewProj[3] - viewProj[1],
			viewProj[7] - viewProj[5],
			viewProj[11] - viewProj[9],
			viewProj[15] - viewProj[13],
		}.Normalize(),

		//bottom plane
		glm.Vec4f{
			viewProj[3] + viewProj[1],
			viewProj[7] + viewProj[5],
			viewProj[11] + viewProj[9],
			viewProj[15] + viewProj[13],
		}.Normalize(),

		//near plane
		glm.Vec4f{
			viewProj[3] + viewProj[2],
			viewProj[7] + viewProj[6],
			viewProj[11] + viewProj[10],
			viewProj[15] + viewProj[14],
		}.Normalize(),

		//far plane
		glm.Vec4f{
			viewProj[3] - viewProj[2],
			viewProj[7] - viewProj[6],
			viewProj[11] - viewProj[10],
			viewProj[15] - viewProj[14],
		}.Normalize(),
	}
}

func (f Frustum) ContainsSphere(sphere Sphere) bool {
	for _, p := range f {
		distance :=
			p[0]*sphere.Center[0] +
				p[1]*sphere.Center[1] +
				p[2]*sphere.Center[2] +
				p[3]

		if distance < -sphere.Radius {
			return false // outside
		}
	}
	return true // inside or intersecting
}
