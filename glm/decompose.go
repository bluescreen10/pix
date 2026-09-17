package glm

import "github.com/chewxy/math32"

// DecomposeMat4f extracts translation, rotation, and per-axis scale from m,
// the inverse of Transform(scale, rot, pos). Assumes m is a plain product of
// translation, rotation, and per-axis scale (no shear) — the scale extracted
// per row is a magnitude, so a matrix built with an odd number of negated
// axes decomposes to a rotation absorbing the sign and a positive scale
// rather than recovering the original negative axis.
func DecomposeMat4f(m Mat4f) (pos Vec3f, rot Quatf, scale Vec3f) {
	pos = Vec3f{m[12], m[13], m[14]}

	row0 := Vec3f{m[0], m[4], m[8]}
	row1 := Vec3f{m[1], m[5], m[9]}
	row2 := Vec3f{m[2], m[6], m[10]}

	sx, sy, sz := row0.Length(), row1.Length(), row2.Length()
	scale = Vec3f{sx, sy, sz}

	if sx > 0 {
		row0 = row0.Scale(1 / sx)
	}
	if sy > 0 {
		row1 = row1.Scale(1 / sy)
	}
	if sz > 0 {
		row2 = row2.Scale(1 / sz)
	}

	r00, r01, r02 := row0[0], row0[1], row0[2]
	r10, r11, r12 := row1[0], row1[1], row1[2]
	r20, r21, r22 := row2[0], row2[1], row2[2]

	trace := r00 + r11 + r22
	switch {
	case trace > 0:
		s := math32.Sqrt(trace+1) * 2
		rot = Quatf{(r21 - r12) / s, (r02 - r20) / s, (r10 - r01) / s, 0.25 * s}
	case r00 > r11 && r00 > r22:
		s := math32.Sqrt(1+r00-r11-r22) * 2
		rot = Quatf{0.25 * s, (r01 + r10) / s, (r02 + r20) / s, (r21 - r12) / s}
	case r11 > r22:
		s := math32.Sqrt(1+r11-r00-r22) * 2
		rot = Quatf{(r01 + r10) / s, 0.25 * s, (r12 + r21) / s, (r02 - r20) / s}
	default:
		s := math32.Sqrt(1+r22-r00-r11) * 2
		rot = Quatf{(r02 + r20) / s, (r12 + r21) / s, 0.25 * s, (r10 - r01) / s}
	}

	return pos, rot, scale
}
