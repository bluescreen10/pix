package glm

// FrustumPlanes extracts the 6 normalized inward frustum planes (Gribb-Hartmann;
// near = row2 for Vulkan/glm 0..1 clip depth) from a column-major view-projection.
func FrustumPlanes(viewProjection Mat4f) [6]Vec4f {
	row0 := viewProjection.Row(0)
	row1 := viewProjection.Row(1)
	row2 := viewProjection.Row(2)
	row3 := viewProjection.Row(3)

	planes := [6]Vec4f{
		row3.Add(row0), // left
		row3.Sub(row0), // right
		row3.Add(row1), // bottom
		row3.Sub(row1), // top
		row2,           // near
		row3.Sub(row2), // far
	}
	for i, plane := range planes {
		length := plane.Vec3().Length()
		if length > 0 {
			planes[i] = plane.Scale(1 / length)
		}
	}
	return planes
}
