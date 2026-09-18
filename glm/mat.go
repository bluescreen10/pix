package glm

// Mat4 is a 4×4 matrix stored in column-major order.
type Mat4[T number] [16]T

// Mul4x4 returns the matrix product of m and other.
func (m Mat4[T]) Mul4x4(other Mat4[T]) Mat4[T] {
	return Mat4[T]{
		// Column 0
		m[0]*other[0] + m[4]*other[1] + m[8]*other[2] + m[12]*other[3],
		m[1]*other[0] + m[5]*other[1] + m[9]*other[2] + m[13]*other[3],
		m[2]*other[0] + m[6]*other[1] + m[10]*other[2] + m[14]*other[3],
		m[3]*other[0] + m[7]*other[1] + m[11]*other[2] + m[15]*other[3],

		// Column 1
		m[0]*other[4] + m[4]*other[5] + m[8]*other[6] + m[12]*other[7],
		m[1]*other[4] + m[5]*other[5] + m[9]*other[6] + m[13]*other[7],
		m[2]*other[4] + m[6]*other[5] + m[10]*other[6] + m[14]*other[7],
		m[3]*other[4] + m[7]*other[5] + m[11]*other[6] + m[15]*other[7],

		// Column 2
		m[0]*other[8] + m[4]*other[9] + m[8]*other[10] + m[12]*other[11],
		m[1]*other[8] + m[5]*other[9] + m[9]*other[10] + m[13]*other[11],
		m[2]*other[8] + m[6]*other[9] + m[10]*other[10] + m[14]*other[11],
		m[3]*other[8] + m[7]*other[9] + m[11]*other[10] + m[15]*other[11],

		// Column 3
		m[0]*other[12] + m[4]*other[13] + m[8]*other[14] + m[12]*other[15],
		m[1]*other[12] + m[5]*other[13] + m[9]*other[14] + m[13]*other[15],
		m[2]*other[12] + m[6]*other[13] + m[10]*other[14] + m[14]*other[15],
		m[3]*other[12] + m[7]*other[13] + m[11]*other[14] + m[15]*other[15],
	}
}

// Transpose returns the transpose of m.
func (m Mat4[T]) Transpose() Mat4[T] {
	return Mat4[T]{
		m[0], m[4], m[8], m[12],
		m[1], m[5], m[9], m[13],
		m[2], m[6], m[10], m[14],
		m[3], m[7], m[11], m[15],
	}
}

// Row returns row num of m. It panics when num is outside [0, 3].
func (m Mat4[T]) Row(num int) Vec4[T] {
	return Vec4[T]{m[num], m[4+num], m[8+num], m[12+num]}
}

// Inv returns the inverse of m. It returns the zero matrix when m is singular.
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
		return Mat4[T]{}
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

// Mul4x1 returns the product of m and vector.
func (m Mat4[T]) Mul4x1(vector Vec4[T]) Vec4[T] {
	return Vec4[T]{
		m[0]*vector[0] + m[4]*vector[1] + m[8]*vector[2] + m[12]*vector[3],
		m[1]*vector[0] + m[5]*vector[1] + m[9]*vector[2] + m[13]*vector[3],
		m[2]*vector[0] + m[6]*vector[1] + m[10]*vector[2] + m[14]*vector[3],
		m[3]*vector[0] + m[7]*vector[1] + m[11]*vector[2] + m[15]*vector[3],
	}
}

// Mat3 returns the upper-left 3×3 portion of m.
func (m Mat4[T]) Mat3() Mat3[T] {
	return Mat3[T]{
		m[0], m[1], m[2],
		m[4], m[5], m[6],
		m[8], m[9], m[10],
	}
}

// Mat4Identity returns a 4×4 identity matrix.
func Mat4Identity[T number]() Mat4[T] {
	return Mat4[T]{
		1, 0, 0, 0,
		0, 1, 0, 0,
		0, 0, 1, 0,
		0, 0, 0, 1,
	}
}

// Mat3 is a 3×3 matrix stored in column-major order.
type Mat3[T number] [9]T

// Transpose returns the transpose of m.
func (m Mat3[T]) Transpose() Mat3[T] {
	return Mat3[T]{
		m[0], m[3], m[6],
		m[1], m[4], m[7],
		m[2], m[5], m[8],
	}
}

// Mul returns the matrix product of m and other.
func (m Mat3[T]) Mul(other Mat3[T]) Mat3[T] {
	var out Mat3[T]
	for column := 0; column < 3; column++ {
		for row := 0; row < 3; row++ {
			var sum T
			for k := 0; k < 3; k++ {
				sum += m[k*3+row] * other[column*3+k]
			}
			out[column*3+row] = sum
		}
	}
	return out
}

// MulVec3 returns the product of m and vector.
func (m Mat3[T]) MulVec3(vector Vec3[T]) Vec3[T] {
	return Vec3[T]{
		m[0]*vector[0] + m[3]*vector[1] + m[6]*vector[2],
		m[1]*vector[0] + m[4]*vector[1] + m[7]*vector[2],
		m[2]*vector[0] + m[5]*vector[1] + m[8]*vector[2],
	}
}

// Mat4 returns m embedded in a 4×4 homogeneous matrix.
func (m Mat3[T]) Mat4() Mat4[T] {
	return Mat4[T]{
		m[0], m[1], m[2], 0,
		m[3], m[4], m[5], 0,
		m[6], m[7], m[8], 0,
		0, 0, 0, 1,
	}
}

// Mat4f is a 4×4 float32 matrix.
type Mat4f = Mat4[float32]

// Mat4fIdentity is the float32 4×4 identity matrix.
var Mat4fIdentity = Mat4Identity[float32]()
