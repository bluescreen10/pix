package glm

import (
	"math"

	"github.com/chewxy/math32"
	"golang.org/x/exp/constraints"
)

type Number interface {
	constraints.Float | constraints.Signed
}

// Vec2
type Vec2[T Number] [2]T

func (v Vec2[T]) X() T {
	return v[0]
}

func (v Vec2[T]) Y() T {
	return v[1]
}

func (v Vec2[T]) Sub(v2 Vec2[T]) Vec2[T] {
	return Vec2[T]{v[0] - v2[0], v[1] - v2[1]}
}

func (v Vec2[T]) Scale(s T) Vec2[T] {
	return Vec2[T]{v[0] * s, v[1] * s}
}

// Vec3
type Vec3[T Number] [3]T

func (v Vec3[T]) X() T {
	return v[0]
}

func (v Vec3[T]) Y() T {
	return v[1]
}

func (v Vec3[T]) Z() T {
	return v[2]
}

func (v Vec3[T]) R() T {
	return v[0]
}

func (v Vec3[T]) G() T {
	return v[1]
}

func (v Vec3[T]) B() T {
	return v[1]
}

func (v Vec3[T]) Normalize() Vec3[T] {
	switch any(v[0]).(type) {
	case float32:
		l := math32.Sqrt(float32(v.Dot(v)))
		return Vec3[T]{
			T(float32(v[0]) / l),
			T(float32(v[1]) / l),
			T(float32(v[2]) / l),
		}
	default:
		l := math.Sqrt(float64((v.Dot(v))))
		return Vec3[T]{
			T(float64(v[0]) / l),
			T(float64(v[1]) / l),
			T(float64(v[2]) / l),
		}
	}
}

func (v Vec3[T]) Cross(v2 Vec3[T]) Vec3[T] {
	return Vec3[T]{
		v[1]*v2[2] - v[2]*v2[1],
		v[2]*v2[0] - v[0]*v2[2],
		v[0]*v2[1] - v[1]*v2[0],
	}
}

func (v Vec3[T]) Dot(v2 Vec3[T]) T {
	return v[0]*v2[0] + v[1]*v2[1] + v[2]*v2[2]
}

func (v Vec3[T]) Scale(s T) Vec3[T] {
	return Vec3[T]{
		v[0] * s,
		v[1] * s,
		v[2] * s,
	}
}

func (v Vec3[T]) Add(v2 Vec3[T]) Vec3[T] {
	return Vec3[T]{
		v[0] + v2[0],
		v[1] + v2[1],
		v[2] + v2[2],
	}
}

func (v Vec3[T]) Sub(v2 Vec3[T]) Vec3[T] {
	return Vec3[T]{
		v[0] - v2[0],
		v[1] - v2[1],
		v[2] - v2[2],
	}
}

func (v Vec3[T]) Rotate(angle T, v2 Vec3[T]) Vec3[T] {
	rot := NewQuat(angle, v2)
	conj := rot.Conjugate()
	return rot.Mul3x1(v).Mul(conj).Vec3()
}

func (v Vec3[T]) Vec4() Vec4[T] {
	return Vec4[T]{v[0], v[1], v[2]}
}

// Vec4
type Vec4[T Number] [4]T

func (v Vec4[T]) X() T {
	return v[0]
}

func (v Vec4[T]) Y() T {
	return v[1]
}

func (v Vec4[T]) Z() T {
	return v[2]
}

func (v Vec4[T]) W() T {
	return v[3]
}

func (v Vec4[T]) R() T {
	return v[0]
}

func (v Vec4[T]) G() T {
	return v[1]
}

func (v Vec4[T]) B() T {
	return v[2]
}

func (v Vec4[T]) A() T {
	return v[3]
}

func (v Vec4[T]) Vec3() Vec3[T] {
	return Vec3[T]{v[0], v[1], v[2]}
}

// aliases
type Vec4f = Vec4[float32]
type Vec4i = Vec4[int32]
type Vec3f = Vec3[float32]
type Vec3i = Vec3[int32]
type Vec2f = Vec2[float32]
type Vec2i = Vec2[int32]
