package glm_test

import (
	"testing"

	"github.com/bluescreen10/pix/glm"
)

func TestMat4IdentityOperations(t *testing.T) {
	matrix := glm.Mat4f{
		1, 2, 3, 4,
		5, 6, 7, 8,
		9, 10, 11, 12,
		13, 14, 15, 16,
	}
	if got := matrix.Mul4x4(glm.Mat4fIdentity); got != matrix {
		t.Errorf("Mul4x4(identity) = %v, want %v", got, matrix)
	}
	if got := glm.Mat4fIdentity.Mul4x4(matrix); got != matrix {
		t.Errorf("identity.Mul4x4() = %v, want %v", got, matrix)
	}
	if got := matrix.Transpose().Transpose(); got != matrix {
		t.Errorf("double transpose = %v, want %v", got, matrix)
	}
	if got, want := matrix.Row(2), (glm.Vec4f{3, 7, 11, 15}); got != want {
		t.Errorf("Row(2) = %v, want %v", got, want)
	}
	vector := glm.Vec4f{1, 2, 3, 4}
	if got := glm.Mat4fIdentity.Mul4x1(vector); got != vector {
		t.Errorf("identity.Mul4x1() = %v, want %v", got, vector)
	}
}

func TestMat4Inverse(t *testing.T) {
	matrix := glm.Transform(
		glm.Vec3f{2, 3, 4},
		glm.NewQuat(float32(0.8), glm.Vec3f{0, 1, 0}),
		glm.Vec3f{5, 6, 7},
	)
	product := matrix.Mul4x4(matrix.Inv())
	assertMat4Close(t, product, glm.Mat4fIdentity)

	if got := (glm.Mat4f{}).Inv(); got != (glm.Mat4f{}) {
		t.Errorf("zero inverse = %v, want zero matrix", got)
	}
}

func TestMat3Operations(t *testing.T) {
	matrix := glm.Mat3[float32]{
		1, 2, 3,
		4, 5, 6,
		7, 8, 9,
	}
	identity := glm.Mat3[float32]{
		1, 0, 0,
		0, 1, 0,
		0, 0, 1,
	}
	if got := matrix.Mul(identity); got != matrix {
		t.Errorf("Mul(identity) = %v, want %v", got, matrix)
	}
	if got := matrix.Transpose().Transpose(); got != matrix {
		t.Errorf("double transpose = %v, want %v", got, matrix)
	}
	if got := identity.MulVec3(glm.Vec3f{1, 2, 3}); got != (glm.Vec3f{1, 2, 3}) {
		t.Errorf("identity.MulVec3() = %v, want [1 2 3]", got)
	}
	if got, want := matrix.MulVec3(glm.Vec3f{1, 0, 0}), (glm.Vec3f{1, 2, 3}); got != want {
		t.Errorf("MulVec3(first basis vector) = %v, want %v", got, want)
	}
	if got := matrix.Mat4().Mat3(); got != matrix {
		t.Errorf("Mat4().Mat3() = %v, want %v", got, matrix)
	}
}

func assertMat4Close(t *testing.T, got, want glm.Mat4f) {
	t.Helper()
	for i := range got {
		if !closeFloat32(got[i], want[i]) {
			t.Errorf("component %d = %v, want %v", i, got[i], want[i])
		}
	}
}
