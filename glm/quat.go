package glm

import (
	"math"

	"github.com/chewxy/math32"
)

type Quat[T Number] [4]T

func NewQuat[T Number](angle T, v Vec3[T]) Quat[T] {
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

// aliases
type Quatf = Quat[float32]
