package glm_test

import (
	"math"
	"testing"

	"github.com/bluescreen10/pix/glm"
)

func TestAngleConversions(t *testing.T) {
	if got := glm.ToRadians(float32(180)); !closeFloat32(got, math.Pi) {
		t.Errorf("ToRadians(180) = %v, want %v", got, math.Pi)
	}
	if got := glm.ToDegrees(float32(math.Pi)); !closeFloat32(got, 180) {
		t.Errorf("ToDegrees(pi) = %v, want 180", got)
	}
	if got := glm.ToRadians(float64(180)); math.Abs(got-math.Pi) > 1e-12 {
		t.Errorf("ToRadians[float64](180) = %v, want %v", got, math.Pi)
	}
}

func TestClamp(t *testing.T) {
	for _, test := range []struct {
		name  string
		value int
		want  int
	}{
		{name: "below", value: -1, want: 0},
		{name: "inside", value: 5, want: 5},
		{name: "above", value: 11, want: 10},
	} {
		t.Run(test.name, func(t *testing.T) {
			if got := glm.Clamp(test.value, 0, 10); got != test.want {
				t.Errorf("Clamp() = %v, want %v", got, test.want)
			}
		})
	}
}

func TestProjectionDepthMappings(t *testing.T) {
	const (
		near = float32(0.1)
		far  = float32(100)
	)
	perspective := glm.PerspectiveRH(float32(math.Pi/2), 1, near, far)
	assertDepth(t, perspective, -near, 0)
	assertDepth(t, perspective, -far, 1)

	reversed := glm.PerspectiveRevZRH(float32(math.Pi/2), 1, near, far)
	assertDepth(t, reversed, -near, 1)
	assertDepth(t, reversed, -far, 0)

	orthographic := glm.OrthoFullRH(float32(-1), 1, -1, 1, near, far)
	assertDepth(t, orthographic, -near, 0)
	assertDepth(t, orthographic, -far, 1)

	reversedOrthographic := glm.OrthoFullRevZRH(float32(-1), 1, -1, 1, near, far)
	assertDepth(t, reversedOrthographic, -near, 1)
	assertDepth(t, reversedOrthographic, -far, 0)
}

func TestLookAtRH(t *testing.T) {
	view := glm.LookAtRH(
		glm.Vec3f{0, 0, 1},
		glm.Vec3f{0, 0, 0},
		glm.Vec3f{0, 1, 0},
	)
	got := view.Mul4x1(glm.Vec4f{0, 0, 0, 1})
	assertVec4Close(t, got, glm.Vec4f{0, 0, -1, 1})
}

func TestBoundingSphereOf(t *testing.T) {
	if got := glm.BoundingSphereOf(nil); got != (glm.Sphere{}) {
		t.Errorf("BoundingSphereOf(nil) = %v, want zero sphere", got)
	}

	got := glm.BoundingSphereOf([]glm.Vec3f{{-1, 0, 0}, {1, 0, 0}})
	if got.Center != (glm.Vec3f{}) || !closeFloat32(got.Radius, 1) {
		t.Errorf("BoundingSphereOf() = %+v, want center [0 0 0], radius 1", got)
	}
}

func assertDepth(t *testing.T, projection glm.Mat4f, viewZ, want float32) {
	t.Helper()
	clip := projection.Mul4x1(glm.Vec4f{0, 0, viewZ, 1})
	got := clip[2] / clip[3]
	if math.Abs(float64(got-want)) > 1e-5 {
		t.Errorf("depth at z=%v = %v, want %v", viewZ, got, want)
	}
}
