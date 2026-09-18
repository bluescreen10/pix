package glm_test

import (
	"math"
	"testing"

	"github.com/bluescreen10/pix/glm"
)

func TestQuaternionAxisAngleRotation(t *testing.T) {
	rotation := glm.NewQuat(float32(math.Pi/2), glm.Vec3f{0, 0, 1})
	assertVec3Close(t, rotation.Rotate(glm.Vec3f{1, 0, 0}), glm.Vec3f{0, 1, 0})
	assertVec3Close(t, rotation.Conjugate().Rotate(glm.Vec3f{0, 1, 0}), glm.Vec3f{1, 0, 0})
}

func TestQuaternionEulerSingleAxesMatchAxisAngle(t *testing.T) {
	const angle = float32(0.7)
	cases := []struct {
		name      string
		fromEuler glm.Quatf
		axis      glm.Vec3f
	}{
		{
			name:      "roll",
			fromEuler: glm.QuatFromEuler(angle, 0, 0),
			axis:      glm.Vec3f{1, 0, 0},
		},
		{
			name:      "pitch",
			fromEuler: glm.QuatFromEuler(0, angle, 0),
			axis:      glm.Vec3f{0, 1, 0},
		},
		{
			name:      "yaw",
			fromEuler: glm.QuatFromEuler(0, 0, angle),
			axis:      glm.Vec3f{0, 0, 1},
		},
	}

	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			want := glm.NewQuat(angle, test.axis)
			assertQuatClose(t, test.fromEuler, want)
		})
	}
}

func TestQuaternionOperations(t *testing.T) {
	q := glm.Quatf{1, 2, 3, 4}
	if q.X() != 1 || q.Y() != 2 || q.Z() != 3 || q.W() != 4 {
		t.Errorf("components = (%v, %v, %v, %v), want (1, 2, 3, 4)", q.X(), q.Y(), q.Z(), q.W())
	}
	if got, want := q.Vec3(), (glm.Vec3f{1, 2, 3}); got != want {
		t.Errorf("Vec3() = %v, want %v", got, want)
	}
	if got, want := q.Dot(q), float32(30); got != want {
		t.Errorf("Dot() = %v, want %v", got, want)
	}
	if got := q.Mul(glm.QuatfIdentity); got != q {
		t.Errorf("Mul(identity) = %v, want %v", got, q)
	}
	if got := q.Normalize().Dot(q.Normalize()); !closeFloat32(got, 1) {
		t.Errorf("normalized dot = %v, want 1", got)
	}
}

func TestSlerpEndpointsAndMidpoint(t *testing.T) {
	start := glm.QuatfIdentity
	end := glm.NewQuat(float32(math.Pi/2), glm.Vec3f{0, 0, 1})

	assertQuatRotationClose(t, glm.Slerp(start, end, 0), start)
	assertQuatRotationClose(t, glm.Slerp(start, end, 1), end)
	sqrtHalf := float32(math.Sqrt(0.5))
	assertVec3Close(
		t,
		glm.Slerp(start, end, 0.5).Rotate(glm.Vec3f{1, 0, 0}),
		glm.Vec3f{sqrtHalf, sqrtHalf, 0},
	)
}

func assertQuatClose(t *testing.T, got, want glm.Quatf) {
	t.Helper()
	for i := range got {
		if !closeFloat32(got[i], want[i]) {
			t.Errorf("component %d = %v, want %v", i, got[i], want[i])
		}
	}
}

func assertQuatRotationClose(t *testing.T, got, want glm.Quatf) {
	t.Helper()
	if got.Dot(want) < 0 {
		got = glm.Quatf{-got[0], -got[1], -got[2], -got[3]}
	}
	assertQuatClose(t, got, want)
}
