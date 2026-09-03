package pix

import (
	"testing"

	"github.com/bluescreen10/pix/glm"
)

// TestPrimitiveWindingIsOutward is the check that actually matters for these
// generators: a triangle wound the wrong way makes the shape render inside-out
// (or vanish under backface culling) with nothing else looking wrong. For every
// primitive, each triangle's geometric normal — from the winding, via the right-
// hand rule — must agree with the vertex normals it was authored with.
func TestPrimitiveWindingIsOutward(t *testing.T) {
	cases := []struct {
		name string
		cfg  GeometryConfig
	}{
		{"box", BoxGeometry(2, 3, 4)},
		{"sphere", SphereGeometry(1.5, 16, 12)},
		{"cylinder", CylinderGeometry(1, 1, 3, 16, 2)},
		{"cone", CylinderGeometry(0, 1, 2, 16, 1)},
		{"capsule", CapsuleGeometry(0.5, 2, 6, 16)},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			pos := attrData[glm.Vec3f](t, tc.cfg, AttributePosition)
			nrm := attrData[glm.Vec3f](t, tc.cfg, AttributeNormal)
			if len(pos) != len(nrm) {
				t.Fatalf("positions %d != normals %d", len(pos), len(nrm))
			}
			if len(tc.cfg.Indices)%3 != 0 || len(tc.cfg.Indices) == 0 {
				t.Fatalf("index count %d is not a positive multiple of 3", len(tc.cfg.Indices))
			}
			for i := 0; i < len(tc.cfg.Indices); i += 3 {
				i0, i1, i2 := tc.cfg.Indices[i], tc.cfg.Indices[i+1], tc.cfg.Indices[i+2]
				a, b, c := pos[i0], pos[i1], pos[i2]
				face := b.Sub(a).Cross(c.Sub(a))
				if face.Length() < 1e-9 {
					t.Fatalf("triangle %d (%d,%d,%d) is degenerate", i/3, i0, i1, i2)
				}
				// Average the corners' authored normals and compare directions.
				avg := nrm[i0].Add(nrm[i1]).Add(nrm[i2])
				if d := face.Normalize().Dot(avg.Normalize()); d <= 0 {
					t.Fatalf("triangle %d (%d,%d,%d) winds inward: face·normal = %v", i/3, i0, i1, i2, d)
				}
			}
		})
	}
}

// TestPrimitiveExtents checks each generator actually honours its dimensions —
// a capsule in particular must be length + 2*radius tall, not length tall.
func TestPrimitiveExtents(t *testing.T) {
	cases := []struct {
		name                string
		cfg                 GeometryConfig
		wantW, wantH, wantD float32
	}{
		{"box", BoxGeometry(2, 3, 4), 2, 3, 4},
		{"sphere", SphereGeometry(1.5, 24, 16), 3, 3, 3},
		{"cylinder", CylinderGeometry(1, 1, 3, 24, 1), 2, 3, 2},
		{"capsule", CapsuleGeometry(0.5, 2, 8, 24), 1, 3, 1}, // 2 + 2*0.5
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			pos := attrData[glm.Vec3f](t, tc.cfg, AttributePosition)
			var lo, hi glm.Vec3f
			for i, p := range pos {
				if i == 0 {
					lo, hi = p, p
					continue
				}
				for k := range 3 {
					lo[k] = min(lo[k], p[k])
					hi[k] = max(hi[k], p[k])
				}
			}
			got := hi.Sub(lo)
			want := glm.Vec3f{tc.wantW, tc.wantH, tc.wantD}
			for k := range 3 {
				if diff := got[k] - want[k]; diff > 0.02 || diff < -0.02 {
					t.Fatalf("extent[%d] = %v, want %v (full extents %v vs %v)", k, got[k], want[k], got, want)
				}
				// Centered on the origin: the two sides should match.
				if sum := hi[k] + lo[k]; sum > 0.02 || sum < -0.02 {
					t.Fatalf("axis %d is not centered on the origin: lo %v hi %v", k, lo[k], hi[k])
				}
			}
		})
	}
}

// attrData pulls one attribute's typed data back out of a GeometryConfig.
func attrData[T any](t *testing.T, cfg GeometryConfig, want AttributeType) []T {
	t.Helper()
	for _, a := range cfg.Attributes {
		if a.attrType == want {
			return fromBytes[T](a.data, a.count)
		}
	}
	t.Fatalf("geometry has no attribute %d", want)
	return nil
}
