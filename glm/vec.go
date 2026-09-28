package glm

import (
	"math"

	"github.com/chewxy/math32"
)

// Vec2 is a two-component vector.
type Vec2[T number] [2]T

// X returns the vector's x component.
func (v Vec2[T]) X() T {
	return v[0]
}

// Y returns the vector's y component.
func (v Vec2[T]) Y() T {
	return v[1]
}

// Add returns the component-wise sum of v and other.
func (v Vec2[T]) Add(other Vec2[T]) Vec2[T] {
	return Vec2[T]{v[0] + other[0], v[1] + other[1]}
}

// Sub returns the component-wise difference between v and other.
func (v Vec2[T]) Sub(other Vec2[T]) Vec2[T] {
	return Vec2[T]{v[0] - other[0], v[1] - other[1]}
}

// Scale returns v with every component multiplied by scalar.
func (v Vec2[T]) Scale(scalar T) Vec2[T] {
	return Vec2[T]{v[0] * scalar, v[1] * scalar}
}

// Dot returns the dot product of v and other.
func (v Vec2[T]) Dot(other Vec2[T]) T {
	return v[0]*other[0] + v[1]*other[1]
}

// Length returns the Euclidean length of v.
func (v Vec2[T]) Length() T {
	switch any(v[0]).(type) {
	case float32:
		return T(math32.Sqrt(float32(v.Dot(v))))
	default:
		return T(math.Sqrt(float64(v.Dot(v))))
	}
}

// Normalize returns a unit-length vector in the direction of v.
// The caller must provide a nonzero vector.
func (v Vec2[T]) Normalize() Vec2[T] {
	length := v.Length()
	return Vec2[T]{v[0] / length, v[1] / length}
}

// Vec3 is a three-component vector.
type Vec3[T number] [3]T

// X returns the vector's x component.
func (v Vec3[T]) X() T {
	return v[0]
}

// Y returns the vector's y component.
func (v Vec3[T]) Y() T {
	return v[1]
}

// Z returns the vector's z component.
func (v Vec3[T]) Z() T {
	return v[2]
}

// Normalize returns a unit-length vector in the direction of v.
// The caller must provide a nonzero vector.
func (v Vec3[T]) Normalize() Vec3[T] {
	length := v.Length()
	return Vec3[T]{v[0] / length, v[1] / length, v[2] / length}
}

// Cross returns the cross product of v and other.
func (v Vec3[T]) Cross(other Vec3[T]) Vec3[T] {
	return Vec3[T]{
		v[1]*other[2] - v[2]*other[1],
		v[2]*other[0] - v[0]*other[2],
		v[0]*other[1] - v[1]*other[0],
	}
}

// Dot returns the dot product of v and other.
func (v Vec3[T]) Dot(other Vec3[T]) T {
	return v[0]*other[0] + v[1]*other[1] + v[2]*other[2]
}

// Length returns the Euclidean length of v.
func (v Vec3[T]) Length() T {
	switch any(v[0]).(type) {
	case float32:
		return T(math32.Sqrt(float32(v.Dot(v))))
	default:
		return T(math.Sqrt(float64(v.Dot(v))))
	}
}

// Scale returns v with every component multiplied by scalar.
func (v Vec3[T]) Scale(scalar T) Vec3[T] {
	return Vec3[T]{
		v[0] * scalar,
		v[1] * scalar,
		v[2] * scalar,
	}
}

// Add returns the component-wise sum of v and other.
func (v Vec3[T]) Add(other Vec3[T]) Vec3[T] {
	return Vec3[T]{
		v[0] + other[0],
		v[1] + other[1],
		v[2] + other[2],
	}
}

// Sub returns the component-wise difference between v and other.
func (v Vec3[T]) Sub(other Vec3[T]) Vec3[T] {
	return Vec3[T]{
		v[0] - other[0],
		v[1] - other[1],
		v[2] - other[2],
	}
}

// Rotate returns v rotated by angle radians around axis.
// The caller must provide a unit-length axis.
func (v Vec3[T]) Rotate(angle T, axis Vec3[T]) Vec3[T] {
	rotation := NewQuat(angle, axis)
	conjugate := rotation.Conjugate()
	return rotation.Mul3x1(v).Mul(conjugate).Vec3()
}

// Unorm10x3 quantizes three values in [0,1] into a packed 32-bit triple, clamping
// out-of-range input. Rounds to nearest rather than truncating: truncation biases
// every channel down by up to one step.
func (v Vec3[T]) Unorm10x3() Unorm10x3 {
	x := uint32(float32(Clamp(v[0], 0, 1))*1023.0 + 0.5)
	y := uint32(float32(Clamp(v[1], 0, 1))*1023.0 + 0.5)
	z := uint32(float32(Clamp(v[2], 0, 1))*1023.0 + 0.5)
	return Unorm10x3(x | y<<10 | z<<20)
}

// Vec4 returns v extended with a zero w component.
func (v Vec3[T]) Vec4(z T) Vec4[T] {
	return Vec4[T]{v[0], v[1], v[2], z}
}

// Vec4 is a four-component vector.
type Vec4[T number] [4]T

// X returns the vector's x component.
func (v Vec4[T]) X() T {
	return v[0]
}

// Y returns the vector's y component.
func (v Vec4[T]) Y() T {
	return v[1]
}

// Z returns the vector's z component.
func (v Vec4[T]) Z() T {
	return v[2]
}

// W returns the vector's w component.
func (v Vec4[T]) W() T {
	return v[3]
}

// Add returns the component-wise sum of v and other.
func (v Vec4[T]) Add(other Vec4[T]) Vec4[T] {
	return Vec4[T]{
		v[0] + other[0],
		v[1] + other[1],
		v[2] + other[2],
		v[3] + other[3],
	}
}

// Sub returns the component-wise difference between v and other.
func (v Vec4[T]) Sub(other Vec4[T]) Vec4[T] {
	return Vec4[T]{
		v[0] - other[0],
		v[1] - other[1],
		v[2] - other[2],
		v[3] - other[3],
	}
}

// Scale returns v with every component multiplied by scalar.
func (v Vec4[T]) Scale(scalar T) Vec4[T] {
	return Vec4[T]{
		v[0] * scalar,
		v[1] * scalar,
		v[2] * scalar,
		v[3] * scalar,
	}
}

// Dot returns the dot product of v and other.
func (v Vec4[T]) Dot(other Vec4[T]) T {
	return v[0]*other[0] + v[1]*other[1] + v[2]*other[2] + v[3]*other[3]
}

// Length returns the Euclidean length of v.
func (v Vec4[T]) Length() T {
	switch any(v[0]).(type) {
	case float32:
		return T(math32.Sqrt(float32(v.Dot(v))))
	default:
		return T(math.Sqrt(float64(v.Dot(v))))
	}
}

// Normalize returns a unit-length vector in the direction of v.
// The caller must provide a nonzero vector.
func (v Vec4[T]) Normalize() Vec4[T] {
	length := v.Length()
	return Vec4[T]{v[0] / length, v[1] / length, v[2] / length, v[3] / length}
}

// Vec3 returns the first three components of v.
func (v Vec4[T]) Vec3() Vec3[T] {
	return Vec3[T]{v[0], v[1], v[2]}
}

// Vec4f is a four-component float32 vector.
type Vec4f = Vec4[float32]

// Vec4i is a four-component int32 vector.
type Vec4i = Vec4[int32]

// Vec3f is a three-component float32 vector.
type Vec3f = Vec3[float32]

// Vec3i is a three-component int32 vector.
type Vec3i = Vec3[int32]

// Vec2f is a two-component float32 vector.
type Vec2f = Vec2[float32]

// Vec2i is a two-component int32 vector.
type Vec2i = Vec2[int32]
