package glm

// items stored in column major
type Mat4[T Number] [16]T

func (m Mat4[T]) Mul4x4(m2 Mat4[T]) Mat4[T] {
	return Mat4[T]{
		// Column 0
		m[0]*m2[0] + m[4]*m2[1] + m[8]*m2[2] + m[12]*m2[3],
		m[1]*m2[0] + m[5]*m2[1] + m[9]*m2[2] + m[13]*m2[3],
		m[2]*m2[0] + m[6]*m2[1] + m[10]*m2[2] + m[14]*m2[3],
		m[3]*m2[0] + m[7]*m2[1] + m[11]*m2[2] + m[15]*m2[3],

		// Column 1
		m[0]*m2[4] + m[4]*m2[5] + m[8]*m2[6] + m[12]*m2[7],
		m[1]*m2[4] + m[5]*m2[5] + m[9]*m2[6] + m[13]*m2[7],
		m[2]*m2[4] + m[6]*m2[5] + m[10]*m2[6] + m[14]*m2[7],
		m[3]*m2[4] + m[7]*m2[5] + m[11]*m2[6] + m[15]*m2[7],

		// Column 2
		m[0]*m2[8] + m[4]*m2[9] + m[8]*m2[10] + m[12]*m2[11],
		m[1]*m2[8] + m[5]*m2[9] + m[9]*m2[10] + m[13]*m2[11],
		m[2]*m2[8] + m[6]*m2[9] + m[10]*m2[10] + m[14]*m2[11],
		m[3]*m2[8] + m[7]*m2[9] + m[11]*m2[10] + m[15]*m2[11],

		// Column 3
		m[0]*m2[12] + m[4]*m2[13] + m[8]*m2[14] + m[12]*m2[15],
		m[1]*m2[12] + m[5]*m2[13] + m[9]*m2[14] + m[13]*m2[15],
		m[2]*m2[12] + m[6]*m2[13] + m[10]*m2[14] + m[14]*m2[15],
		m[3]*m2[12] + m[7]*m2[13] + m[11]*m2[14] + m[15]*m2[15],
	}
}

func (m Mat4[T]) Transpose() Mat4[T] {
	return Mat4[T]{
		m[0], m[4], m[8], m[12],
		m[1], m[5], m[9], m[13],
		m[2], m[6], m[10], m[14],
		m[3], m[7], m[11], m[15],
	}
}

func (m Mat4[T]) Inv() Mat4[T] {
	// Calculate all 2x2 determinants (sub-determinants)
	s0 := m[0]*m[5] - m[1]*m[4]
	s1 := m[0]*m[6] - m[2]*m[4]
	s2 := m[0]*m[7] - m[3]*m[4]
	s3 := m[1]*m[6] - m[2]*m[5]
	s4 := m[1]*m[7] - m[3]*m[5]
	s5 := m[2]*m[7] - m[3]*m[6]

	c5 := m[10]*m[15] - m[11]*m[14]
	c4 := m[9]*m[15] - m[11]*m[13]
	c3 := m[9]*m[14] - m[10]*m[13]
	c2 := m[8]*m[15] - m[11]*m[12]
	c1 := m[8]*m[14] - m[10]*m[12]
	c0 := m[8]*m[13] - m[9]*m[12]

	// Calculate determinant
	det := s0*c5 - s1*c4 + s2*c3 + s3*c2 - s4*c1 + s5*c0

	if float32(det) < 1e-10 && float32(det) > -1e-10 {
		// Matrix is singular (non-invertible)
		return Mat4[T]{} // or panic/return error
	}

	invDet := 1.0 / det

	// Calculate inverse matrix elements (row-major)
	var inv Mat4[T]
	inv[0] = (m[5]*c5 - m[6]*c4 + m[7]*c3) * invDet
	inv[4] = (-m[4]*c5 + m[6]*c2 - m[7]*c1) * invDet
	inv[8] = (m[4]*c4 - m[5]*c2 + m[7]*c0) * invDet
	inv[12] = (-m[4]*c3 + m[5]*c1 - m[6]*c0) * invDet

	inv[1] = (-m[1]*c5 + m[2]*c4 - m[3]*c3) * invDet
	inv[5] = (m[0]*c5 - m[2]*c2 + m[3]*c1) * invDet
	inv[9] = (-m[0]*c4 + m[1]*c2 - m[3]*c0) * invDet
	inv[13] = (m[0]*c3 - m[1]*c1 + m[2]*c0) * invDet

	inv[2] = (m[13]*s5 - m[14]*s4 + m[15]*s3) * invDet
	inv[6] = (-m[12]*s5 + m[14]*s2 - m[15]*s1) * invDet
	inv[10] = (m[12]*s4 - m[13]*s2 + m[15]*s0) * invDet
	inv[14] = (-m[12]*s3 + m[13]*s1 - m[14]*s0) * invDet

	inv[3] = (-m[9]*s5 + m[10]*s4 - m[11]*s3) * invDet
	inv[7] = (m[8]*s5 - m[10]*s2 + m[11]*s1) * invDet
	inv[11] = (-m[8]*s4 + m[9]*s2 - m[11]*s0) * invDet
	inv[15] = (m[8]*s3 - m[9]*s1 + m[10]*s0) * invDet

	return inv
}

func (m Mat4[T]) Mul4x1(v Vec4[T]) Vec4[T] {
	return Vec4[T]{
		m[0]*v[0] + m[1]*v[1] + m[2]*v[2] + m[3]*v[3],
		m[4]*v[0] + m[5]*v[1] + m[6]*v[2] + m[7]*v[3],
		m[8]*v[0] + m[9]*v[1] + m[10]*v[2] + m[11]*v[3],
		m[12]*v[0] + m[13]*v[1] + m[14]*v[2] + m[15]*v[3],
	}
}

func (m Mat4[T]) Mat3() Mat3[T] {
	return Mat3[T]{
		m[0], m[1], m[2],
		m[4], m[5], m[6],
		m[8], m[9], m[10],
	}
}

func Mat4Identity[T Number]() Mat4[T] {
	return Mat4[T]{
		1, 0, 0, 0,
		0, 1, 0, 0,
		0, 0, 1, 0,
		0, 0, 0, 1,
	}
}

type Mat3[T Number] [9]T

func (m Mat3[T]) Transpose() Mat3[T] {
	return Mat3[T]{
		m[0], m[3], m[6],
		m[1], m[4], m[7],
		m[2], m[5], m[8],
	}
}

func (m Mat3[T]) Mul(m2 Mat3[T]) Mat3[T] {
	var out Mat3[T]
	for row := 0; row < 3; row++ {
		for col := 0; col < 3; col++ {
			var sum T
			for k := 0; k < 3; k++ {
				sum += m[row*3+k] * m2[k*3+col]
			}
			out[row*3+col] = sum
		}
	}
	return out
}

func (m Mat3[T]) MulVec3(v Vec3[T]) Vec3[T] {
	return Vec3[T]{
		m[0]*v[0] + m[1]*v[1] + m[2]*v[2],
		m[3]*v[0] + m[4]*v[1] + m[5]*v[2],
		m[6]*v[0] + m[7]*v[1] + m[8]*v[2],
	}
}

// aliases
type Mat4f = Mat4[float32]

var Mat4fIndentity = Mat4Identity[float32]
