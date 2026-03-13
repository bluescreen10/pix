package glm

func Transform[T number](scale Vec3[T], rot Quat[T], pos Vec3[T]) Mat4[T] {

	xx, yy, zz := rot[0]*rot[0], rot[1]*rot[1], rot[2]*rot[2]
	xy, xz, yz := rot[0]*rot[1], rot[0]*rot[2], rot[1]*rot[2]
	wx, wy, wz := rot[3]*rot[0], rot[3]*rot[1], rot[3]*rot[2]

	return Mat4[T]{
		(1 - 2*(yy+zz)) * scale[0], 2 * (xy + wz) * scale[1], 2 * (xz - wy) * scale[2], 0,
		2 * (xy - wz) * scale[0], (1 - 2*(xx+zz)) * scale[1], 2 * (yz + wx) * scale[2], 0,
		2 * (xz + wy) * scale[0], 2 * (yz - wx) * scale[1], (1 - 2*(xx+yy)) * scale[2], 0,
		pos[0], pos[1], pos[2], 1,
	}
}
