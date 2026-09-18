package glm_test

import (
	"math"
	"testing"

	"github.com/bluescreen10/pix/glm"
)

func TestVec2Operations(t *testing.T) {
	left := glm.Vec2f{3, 4}
	right := glm.Vec2f{1, 2}

	if got, want := left.Add(right), (glm.Vec2f{4, 6}); got != want {
		t.Errorf("Add() = %v, want %v", got, want)
	}
	if got, want := left.Sub(right), (glm.Vec2f{2, 2}); got != want {
		t.Errorf("Sub() = %v, want %v", got, want)
	}
	if got, want := left.Scale(2), (glm.Vec2f{6, 8}); got != want {
		t.Errorf("Scale() = %v, want %v", got, want)
	}
	if got, want := left.Dot(right), float32(11); got != want {
		t.Errorf("Dot() = %v, want %v", got, want)
	}
	if got, want := left.Length(), float32(5); got != want {
		t.Errorf("Length() = %v, want %v", got, want)
	}
	assertVec2Close(t, left.Normalize(), glm.Vec2f{0.6, 0.8})
}

func TestVec3Operations(t *testing.T) {
	left := glm.Vec3f{2, 3, 6}
	right := glm.Vec3f{1, 2, 3}

	if got, want := left.Add(right), (glm.Vec3f{3, 5, 9}); got != want {
		t.Errorf("Add() = %v, want %v", got, want)
	}
	if got, want := left.Sub(right), (glm.Vec3f{1, 1, 3}); got != want {
		t.Errorf("Sub() = %v, want %v", got, want)
	}
	if got, want := left.Scale(2), (glm.Vec3f{4, 6, 12}); got != want {
		t.Errorf("Scale() = %v, want %v", got, want)
	}
	if got, want := left.Dot(right), float32(26); got != want {
		t.Errorf("Dot() = %v, want %v", got, want)
	}
	if got, want := left.Length(), float32(7); got != want {
		t.Errorf("Length() = %v, want %v", got, want)
	}
	assertVec3Close(t, left.Normalize(), glm.Vec3f{2.0 / 7.0, 3.0 / 7.0, 6.0 / 7.0})
}

func TestVec4Operations(t *testing.T) {
	left := glm.Vec4f{1, 2, 2, 4}
	right := glm.Vec4f{4, 3, 2, 1}

	if got, want := left.Add(right), (glm.Vec4f{5, 5, 4, 5}); got != want {
		t.Errorf("Add() = %v, want %v", got, want)
	}
	if got, want := left.Sub(right), (glm.Vec4f{-3, -1, 0, 3}); got != want {
		t.Errorf("Sub() = %v, want %v", got, want)
	}
	if got, want := left.Scale(2), (glm.Vec4f{2, 4, 4, 8}); got != want {
		t.Errorf("Scale() = %v, want %v", got, want)
	}
	if got, want := left.Dot(right), float32(18); got != want {
		t.Errorf("Dot() = %v, want %v", got, want)
	}
	if got, want := left.Length(), float32(5); got != want {
		t.Errorf("Length() = %v, want %v", got, want)
	}
	assertVec4Close(t, left.Normalize(), glm.Vec4f{0.2, 0.4, 0.4, 0.8})
}

func TestIntegerVectorOperations(t *testing.T) {
	if got, want := (glm.Vec2i{1, 2}).Add(glm.Vec2i{3, 4}), (glm.Vec2i{4, 6}); got != want {
		t.Errorf("Vec2i.Add() = %v, want %v", got, want)
	}
	if got, want := (glm.Vec3i{1, 2, 3}).Scale(2), (glm.Vec3i{2, 4, 6}); got != want {
		t.Errorf("Vec3i.Scale() = %v, want %v", got, want)
	}
	if got, want := (glm.Vec4i{4, 3, 2, 1}).Sub(glm.Vec4i{1, 1, 1, 1}), (glm.Vec4i{3, 2, 1, 0}); got != want {
		t.Errorf("Vec4i.Sub() = %v, want %v", got, want)
	}
}

func TestVectorComponentsAndConversions(t *testing.T) {
	vec2 := glm.Vec2i{1, 2}
	if vec2.X() != 1 || vec2.Y() != 2 {
		t.Errorf("Vec2 components = (%v, %v), want (1, 2)", vec2.X(), vec2.Y())
	}

	vec3 := glm.Vec3i{1, 2, 3}
	if vec3.X() != 1 || vec3.Y() != 2 || vec3.Z() != 3 {
		t.Errorf("Vec3 components = (%v, %v, %v), want (1, 2, 3)", vec3.X(), vec3.Y(), vec3.Z())
	}
	if got, want := vec3.Vec4(), (glm.Vec4i{1, 2, 3, 0}); got != want {
		t.Errorf("Vec4() = %v, want %v", got, want)
	}

	vec4 := glm.Vec4i{1, 2, 3, 4}
	if vec4.X() != 1 || vec4.Y() != 2 || vec4.Z() != 3 || vec4.W() != 4 {
		t.Errorf("Vec4 components = (%v, %v, %v, %v), want (1, 2, 3, 4)", vec4.X(), vec4.Y(), vec4.Z(), vec4.W())
	}
	if got, want := vec4.Vec3(), vec3; got != want {
		t.Errorf("Vec3() = %v, want %v", got, want)
	}
}

func TestVec3SpecificOperations(t *testing.T) {
	xAxis := glm.Vec3f{1, 0, 0}
	yAxis := glm.Vec3f{0, 1, 0}
	if got, want := xAxis.Cross(yAxis), (glm.Vec3f{0, 0, 1}); got != want {
		t.Errorf("Cross() = %v, want %v", got, want)
	}

	rotated := xAxis.Rotate(float32(math.Pi/2), glm.Vec3f{0, 0, 1})
	assertVec3Close(t, rotated, yAxis)
}

func assertVec2Close(t *testing.T, got, want glm.Vec2f) {
	t.Helper()
	for i := range got {
		if !closeFloat32(got[i], want[i]) {
			t.Errorf("component %d = %v, want %v", i, got[i], want[i])
		}
	}
}

func assertVec3Close(t *testing.T, got, want glm.Vec3f) {
	t.Helper()
	for i := range got {
		if !closeFloat32(got[i], want[i]) {
			t.Errorf("component %d = %v, want %v", i, got[i], want[i])
		}
	}
}

func assertVec4Close(t *testing.T, got, want glm.Vec4f) {
	t.Helper()
	for i := range got {
		if !closeFloat32(got[i], want[i]) {
			t.Errorf("component %d = %v, want %v", i, got[i], want[i])
		}
	}
}

func closeFloat32(left, right float32) bool {
	return math.Abs(float64(left-right)) < 1e-6
}
