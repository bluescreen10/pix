package glm

// Transform returns a column-major matrix that applies scale, rotation, and
// translation in that order.
func Transform[T number](scale Vec3[T], rotation Quat[T], position Vec3[T]) Mat4[T] {
	xx := rotation[0] * rotation[0]
	yy := rotation[1] * rotation[1]
	zz := rotation[2] * rotation[2]
	xy := rotation[0] * rotation[1]
	xz := rotation[0] * rotation[2]
	yz := rotation[1] * rotation[2]
	wx := rotation[3] * rotation[0]
	wy := rotation[3] * rotation[1]
	wz := rotation[3] * rotation[2]

	return Mat4[T]{
		(1 - 2*(yy+zz)) * scale[0], 2 * (xy + wz) * scale[1], 2 * (xz - wy) * scale[2], 0,
		2 * (xy - wz) * scale[0], (1 - 2*(xx+zz)) * scale[1], 2 * (yz + wx) * scale[2], 0,
		2 * (xz + wy) * scale[0], 2 * (yz - wx) * scale[1], (1 - 2*(xx+yy)) * scale[2], 0,
		position[0], position[1], position[2], 1,
	}
}
