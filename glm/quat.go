package glm

import (
	"math"

	"github.com/chewxy/math32"
)

type Quat[T number] [4]T

func NewQuat[T number](angle T, v Vec3[T]) Quat[T] {
	switch any(v[0]).(type) {
	case float32:
		sin, cos := math32.Sincos(float32(angle) / 2)

		return Quat[T]{
			v[0] * T(sin),
			v[1] * T(sin),
			v[2] * T(sin),
			T(cos),
		}
	default:
		sin, cos := math.Sincos(float64(angle) / 2)

		return Quat[T]{
			v[0] * T(sin),
			v[1] * T(sin),
			v[2] * T(sin),
			T(cos),
		}
	}
}

func QuatFromEuler[T number](roll, pitch, yaw T) Quat[T] {
	switch any(roll).(type) {
	case float32:
		sx, cx := math32.Sincos(float32(roll))
		sy, cy := math32.Sincos(float32(pitch))
		sz, cz := math32.Sincos(float32(yaw))

		return Quat[T]{
			T(sx*cy*cz - cx*sy*sz),
			T(cx*sy*cz + sx*cy*sz),
			T(cx*cy*sz - sx*sy*cz),
			T(cx*cy*cz + sx*sy*sz),
		}
	default:
		sx, cx := math.Sincos(float64(roll))
		sy, cy := math.Sincos(float64(pitch))
		sz, cz := math.Sincos(float64(yaw))

		return Quat[T]{
			T(sx*cy*cz - cx*sy*sz),
			T(cx*sy*cz + sx*cy*sz),
			T(cx*cy*sz - sx*sy*cz),
			T(cx*cy*cz + sx*sy*sz),
		}
	}
}

func (q Quat[T]) X() T {
	return q[0]
}

func (q Quat[T]) Y() T {
	return q[1]
}

func (q Quat[T]) Z() T {
	return q[2]
}

func (q Quat[T]) W() T {
	return q[3]
}

func (q Quat[T]) Conjugate() Quat[T] {
	return Quat[T]{
		-q[0],
		-q[1],
		-q[2],
		q[3],
	}
}

func (q Quat[T]) Mul3x1(v Vec3[T]) Quat[T] {
	return Quat[T]{
		(q[3] * v[0]) + (q[1] * v[2]) - (q[2] * v[1]),
		(q[3] * v[1]) + (q[2] * v[0]) - (q[0] * v[2]),
		(q[3] * v[2]) + (q[0] * v[1]) - (q[1] * v[0]),
		-(q[0] * v[0]) - (q[1] * v[1]) - (q[2] * v[2]),
	}
}

func (q Quat[T]) Mul(q2 Quat[T]) Quat[T] {
	return Quat[T]{
		(q[0] * q2[3]) + (q[3] * q2[0]) + (q[1] * q2[2]) - (q[2] * q2[1]),
		(q[1] * q2[3]) + (q[3] * q2[1]) + (q[2] * q2[0]) - (q[0] * q2[2]),
		(q[2] * q2[3]) + (q[3] * q2[2]) + (q[0] * q2[1]) - (q[1] * q2[0]),
		(q[3] * q2[3]) - (q[0] * q2[0]) - (q[1] * q2[1]) - (q[2] * q2[2]),
	}
}

func (q Quat[T]) Vec3() Vec3[T] {
	return Vec3[T]{q[0], q[1], q[2]}
}

func QuatIdentity[T number]() Quat[T] {
	return Quat[T]{0, 0, 0, 1}
}

// aliases
type Quatf = Quat[float32]

var QuatIdentityf = QuatIdentity[float32]()
