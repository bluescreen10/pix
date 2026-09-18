package glm

import (
	"math"
)

// mat4Row returns row i (0-3) of a column-major Mat4f as a plane-equation vector.
func mat4Row(m Mat4f, i int) [4]float32 {
	return [4]float32{m[i], m[4+i], m[8+i], m[12+i]}
}

// addScaledRow returns a + sign*b, treating each row as a plane equation.
func addScaledRow(a, b [4]float32, sign float32) [4]float32 {
	return [4]float32{
		a[0] + sign*b[0],
		a[1] + sign*b[1],
		a[2] + sign*b[2],
		a[3] + sign*b[3],
	}
}

// FrustumPlanes extracts the 6 normalized inward frustum planes (Gribb-Hartmann;
// near = row2 for Vulkan/glm 0..1 clip depth) from a column-major view-projection.
func FrustumPlanes(vp Mat4f) [6]Vec4f {
	row0, row1, row2, row3 := mat4Row(vp, 0), mat4Row(vp, 1), mat4Row(vp, 2), mat4Row(vp, 3)

	planes := [6]Vec4f{
		addScaledRow(row3, row0, 1),  // left
		addScaledRow(row3, row0, -1), // right
		addScaledRow(row3, row1, 1),  // bottom
		addScaledRow(row3, row1, -1), // top
		row2,                         // near
		addScaledRow(row3, row2, -1), // far
	}
	for i, p := range planes {
		length := float32(math.Sqrt(float64(p[0]*p[0] + p[1]*p[1] + p[2]*p[2])))
		if length > 0 {
			planes[i] = Vec4f{p[0] / length, p[1] / length, p[2] / length, p[3] / length}
		}
	}
	return planes
}
