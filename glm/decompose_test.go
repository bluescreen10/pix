package glm

import "testing"

func TestDecomposeMat4fRoundTrip(t *testing.T) {
	cases := []struct {
		name  string
		scale Vec3f
		rot   Quatf
		pos   Vec3f
	}{
		{"identity", Vec3f{1, 1, 1}, QuatfIdentity, Vec3f{0, 0, 0}},
		{"translated", Vec3f{1, 1, 1}, QuatfIdentity, Vec3f{3, -2, 5}},
		{"rotated-y", Vec3f{1, 1, 1}, NewQuat(1.2, Vec3f{0, 1, 0}), Vec3f{0, 0, 0}},
		{"rotated-arbitrary", Vec3f{1, 1, 1}, NewQuat(0.7, Vec3f{0.4, 0.6, 0.7}.Normalize()), Vec3f{1, 2, 3}},
		{"nonuniform-scale", Vec3f{2, 0.5, 3}, NewQuat(0.9, Vec3f{0, 0, 1}), Vec3f{-1, 4, 2}},
		{"full", Vec3f{1.5, 2.5, 0.75}, NewQuat(2.1, Vec3f{1, 1, 1}.Normalize()), Vec3f{10, -5, 2}},
	}

	const eps = 1e-3
	closeV := func(a, b Vec3f) bool {
		return math32Abs(a[0]-b[0]) < eps && math32Abs(a[1]-b[1]) < eps && math32Abs(a[2]-b[2]) < eps
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			m := Transform(c.scale, c.rot, c.pos)
			pos, rot, scale := DecomposeMat4f(m)

			if !closeV(pos, c.pos) {
				t.Errorf("position: got %v, want %v", pos, c.pos)
			}
			if !closeV(scale, c.scale) {
				t.Errorf("scale: got %v, want %v", scale, c.scale)
			}

			// Rotation may recover the negated quaternion (same rotation) —
			// compare via the reconstructed matrix instead of the quat directly.
			got := Transform(c.scale, rot, c.pos)
			for i := range got {
				if math32Abs(got[i]-m[i]) > eps {
					t.Errorf("reconstructed matrix differs at %d: got %v, want %v", i, got[i], m[i])
					break
				}
			}
		})
	}
}

func math32Abs(v float32) float32 {
	if v < 0 {
		return -v
	}
	return v
}
