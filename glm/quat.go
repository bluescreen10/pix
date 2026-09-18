package glm

import (
	"math"

	"github.com/chewxy/math32"
)

// Quat is a quaternion stored as x, y, z, w.
type Quat[T number] [4]T

// NewQuat returns a quaternion for angle radians around axis.
// The caller must provide a unit-length axis.
func NewQuat[T number](angle T, axis Vec3[T]) Quat[T] {
	switch any(axis[0]).(type) {
	case float32:
		sin, cos := math32.Sincos(float32(angle) / 2)

		return Quat[T]{
			axis[0] * T(sin),
			axis[1] * T(sin),
			axis[2] * T(sin),
			T(cos),
		}
	default:
		sin, cos := math.Sincos(float64(angle) / 2)

		return Quat[T]{
			axis[0] * T(sin),
			axis[1] * T(sin),
			axis[2] * T(sin),
			T(cos),
		}
	}
}

// QuatFromEuler returns a quaternion from roll, pitch, and yaw rotations.
func QuatFromEuler[T number](roll, pitch, yaw T) Quat[T] {
	switch any(roll).(type) {
	case float32:
		sx, cx := math32.Sincos(float32(roll) / 2)
		sy, cy := math32.Sincos(float32(pitch) / 2)
		sz, cz := math32.Sincos(float32(yaw) / 2)

		return Quat[T]{
			T(sx*cy*cz - cx*sy*sz),
			T(cx*sy*cz + sx*cy*sz),
			T(cx*cy*sz - sx*sy*cz),
			T(cx*cy*cz + sx*sy*sz),
		}
	default:
		sx, cx := math.Sincos(float64(roll) / 2)
		sy, cy := math.Sincos(float64(pitch) / 2)
		sz, cz := math.Sincos(float64(yaw) / 2)

		return Quat[T]{
			T(sx*cy*cz - cx*sy*sz),
			T(cx*sy*cz + sx*cy*sz),
			T(cx*cy*sz - sx*sy*cz),
			T(cx*cy*cz + sx*sy*sz),
		}
	}
}

// X returns the quaternion's x component.
func (q Quat[T]) X() T {
	return q[0]
}

// Y returns the quaternion's y component.
func (q Quat[T]) Y() T {
	return q[1]
}

// Z returns the quaternion's z component.
func (q Quat[T]) Z() T {
	return q[2]
}

// W returns the quaternion's w component.
func (q Quat[T]) W() T {
	return q[3]
}

// Conjugate returns the conjugate of q.
func (q Quat[T]) Conjugate() Quat[T] {
	return Quat[T]{
		-q[0],
		-q[1],
		-q[2],
		q[3],
	}
}

// Mul3x1 returns q multiplied by vector represented as a pure quaternion.
func (q Quat[T]) Mul3x1(vector Vec3[T]) Quat[T] {
	return Quat[T]{
		(q[3] * vector[0]) + (q[1] * vector[2]) - (q[2] * vector[1]),
		(q[3] * vector[1]) + (q[2] * vector[0]) - (q[0] * vector[2]),
		(q[3] * vector[2]) + (q[0] * vector[1]) - (q[1] * vector[0]),
		-(q[0] * vector[0]) - (q[1] * vector[1]) - (q[2] * vector[2]),
	}
}

// Mul returns the quaternion product of q and other.
func (q Quat[T]) Mul(other Quat[T]) Quat[T] {
	return Quat[T]{
		(q[0] * other[3]) + (q[3] * other[0]) + (q[1] * other[2]) - (q[2] * other[1]),
		(q[1] * other[3]) + (q[3] * other[1]) + (q[2] * other[0]) - (q[0] * other[2]),
		(q[2] * other[3]) + (q[3] * other[2]) + (q[0] * other[1]) - (q[1] * other[0]),
		(q[3] * other[3]) - (q[0] * other[0]) - (q[1] * other[1]) - (q[2] * other[2]),
	}
}

// Rotate returns vector rotated by q.
func (q Quat[T]) Rotate(vector Vec3[T]) Vec3[T] {
	// q * v * q⁻¹
	result := q.Mul3x1(vector).Mul(q.Conjugate())

	return Vec3[T]{
		result.X(),
		result.Y(),
		result.Z(),
	}
}

// Vec3 returns the vector portion of q.
func (q Quat[T]) Vec3() Vec3[T] {
	return Vec3[T]{q[0], q[1], q[2]}
}

// QuatIdentity returns the identity quaternion.
func QuatIdentity[T number]() Quat[T] {
	return Quat[T]{0, 0, 0, 1}
}

// Dot returns the dot product of q and other.
func (q Quat[T]) Dot(other Quat[T]) T {
	return q[0]*other[0] + q[1]*other[1] + q[2]*other[2] + q[3]*other[3]
}

// Normalize returns q scaled to unit length.
// The caller must provide a nonzero quaternion.
func (q Quat[T]) Normalize() Quat[T] {
	switch any(q[0]).(type) {
	case float32:
		inv := T(1.0 / math32.Sqrt(float32(q.Dot(q))))
		return Quat[T]{q[0] * inv, q[1] * inv, q[2] * inv, q[3] * inv}
	default:
		inv := T(1.0 / math.Sqrt(float64(q.Dot(q))))
		return Quat[T]{q[0] * inv, q[1] * inv, q[2] * inv, q[3] * inv}
	}
}

// Slerp interpolates between start and end by amount in [0, 1].
func Slerp(start, end Quatf, amount float32) Quatf {
	cos := start.Dot(end)
	if cos < 0 {
		end = Quatf{-end[0], -end[1], -end[2], -end[3]}
		cos = -cos
	}
	if cos > 0.9995 {
		result := Quatf{
			start[0] + amount*(end[0]-start[0]),
			start[1] + amount*(end[1]-start[1]),
			start[2] + amount*(end[2]-start[2]),
			start[3] + amount*(end[3]-start[3]),
		}
		return result.Normalize()
	}
	theta0 := math32.Acos(cos)
	theta := theta0 * amount
	sinTheta := math32.Sin(theta)
	sinTheta0 := math32.Sin(theta0)
	startScale := math32.Cos(theta) - cos*sinTheta/sinTheta0
	endScale := sinTheta / sinTheta0
	return Quatf{
		startScale*start[0] + endScale*end[0],
		startScale*start[1] + endScale*end[1],
		startScale*start[2] + endScale*end[2],
		startScale*start[3] + endScale*end[3],
	}
}

// Quatf is a float32 quaternion.
type Quatf = Quat[float32]

// QuatfIdentity is the float32 identity quaternion.
var QuatfIdentity = QuatIdentity[float32]()
