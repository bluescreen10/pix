package glm

import (
	"math"

	"github.com/chewxy/math32"
)

func PerspectiveRH[T number](fovYrad, aspectRatio, zNear, zFar T) Mat4[T] {
	sinFov, cosFov := math.Sincos(float64(0.5) * float64(fovYrad))
	h := T(cosFov) / T(sinFov)
	w := h / aspectRatio
	r := zFar / (zNear - zFar)

	// -1 as a runtime value: the untyped constant -1 is rejected because the number
	// constraint includes unsigned types (perspective is only used with floats).
	one := T(1)
	return Mat4[T]{
		w, 0, 0, 0,
		0, h, 0, 0,
		0, 0, r, -one,
		0, 0, r * zNear, 0,
	}
}

// PerspectiveRevZRH is PerspectiveRH with a REVERSED depth range: the near plane
// maps to 1 and the far plane to 0, instead of 0 and 1.
//
// This is what makes a floating-point depth buffer behave. Float precision is
// densest near 0, and the perspective divide crowds depth values toward the far
// plane — with the conventional mapping those two effects compound, so most of the
// buffer's precision lands where nothing needs it. Reversing lines float's dense
// region up with the far plane, where the projection is coarsest, and the two
// cancel into near-uniform precision. It costs nothing: same format, same speed.
//
// Swapping the near and far arguments is the whole implementation — the standard
// formula run with them exchanged produces exactly the reversed mapping. Callers
// still pass near and far in the usual order.
//
// Depth state must agree: clear to 0 (not 1) and compare with Greater (not Less).
func PerspectiveRevZRH[T number](fovYrad, aspectRatio, zNear, zFar T) Mat4[T] {
	return PerspectiveRH(fovYrad, aspectRatio, zFar, zNear)
}

// OrthoFullRevZRH is OrthoFullRH with the same reversed depth range, so
// orthographic (directional shadow) cameras match the convention above. An
// orthographic projection is linear in z, so this buys no precision on its own —
// it exists so every depth buffer in the engine reads the same way.
func OrthoFullRevZRH[T number](left, right, bottom, top, near, far T) Mat4[T] {
	return OrthoFullRH(left, right, bottom, top, far, near)
}

func LookAtRH[T number](eye, center, up Vec3[T]) Mat4[T] {
	f := (center.Sub(eye)).Normalize()
	s := f.Cross(up).Normalize()
	u := s.Cross(f)

	return Mat4[T]{
		s[0], u[0], -f[0], 0,
		s[1], u[1], -f[1], 0,
		s[2], u[2], -f[2], 0,
		-eye.Dot(s), -eye.Dot(u), eye.Dot(f), 1,
	}
}

func OrthoRH[T number](aspectRatio, zNear, zFar T) Mat4[T] {
	h := T(1)
	w := h / aspectRatio
	r := T(1) / (zNear - zFar)

	return Mat4[T]{
		w, 0, 0, 0,
		0, h, 0, 0,
		0, 0, r, 0,
		0, 0, r * zNear, 1,
	}
}

func OrthoFullRH[T number](left, right, bottom, top, near, far T) Mat4[T] {
	return Mat4[T]{
		2 / (right - left), 0, 0, 0,
		0, 2 / (top - bottom), 0, 0,
		0, 0, 1 / (near - far), 0,
		-(right + left) / (right - left), -(top + bottom) / (top - bottom), near / (near - far), 1,
	}
}

func ToRadians[T number](angle T) T {
	switch any(angle).(type) {
	case float32:
		return T(float32(angle) * math32.Pi / 180)
	default:
		return T(float64(angle) * math.Pi / 180)
	}
}

func ToDegrees[T number](angle T) T {
	switch any(angle).(type) {
	case float32:
		return T(float32(angle) * 180 / math32.Pi)
	default:
		return T(float64(angle) * 180 / math.Pi)
	}
}

func Clamp[T number](x, min, max T) T {
	if x < min {
		return min
	} else if x > max {
		return max
	}
	return x
}
