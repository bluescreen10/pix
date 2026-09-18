package glm

import (
	"math"
)

// FrustumPlanes extracts the 6 normalized inward frustum planes (Gribb-Hartmann;
// near = row2 for Vulkan/glm 0..1 clip depth) from a column-major view-projection.
func FrustumPlanes(vp Mat4f) [6]Vec4f {
	row := func(i int) [4]float32 { return [4]float32{vp[i], vp[4+i], vp[8+i], vp[12+i]} }
	r0, r1, r2, r3 := row(0), row(1), row(2), row(3)
	comb := func(a, b [4]float32, sgn float32) [4]float32 {
		return [4]float32{a[0] + sgn*b[0], a[1] + sgn*b[1], a[2] + sgn*b[2], a[3] + sgn*b[3]}
	}
	pl := [6]Vec4f{comb(r3, r0, 1), comb(r3, r0, -1), comb(r3, r1, 1), comb(r3, r1, -1), r2, comb(r3, r2, -1)}
	for i := range pl {
		p := pl[i]
		l := float32(math.Sqrt(float64(p[0]*p[0] + p[1]*p[1] + p[2]*p[2])))
		if l > 0 {
			pl[i] = Vec4f{p[0] / l, p[1] / l, p[2] / l, p[3] / l}
		}
	}
	return pl
}
