package glm

import "math"

// Sphere is a bounding sphere: a center and radius in whatever space it was
// computed (local, world, etc. — the caller tracks which).
type Sphere struct {
	Center Vec3f
	Radius float32
}

// BoundingSphereOf returns the centroid and max-radius bounding sphere of points.
func BoundingSphereOf(points []Vec3f) Sphere {
	var s Sphere
	if len(points) == 0 {
		return s
	}
	for _, p := range points {
		s.Center = s.Center.Add(p)
	}
	s.Center = s.Center.Scale(1.0 / float32(len(points)))
	var maxSq float32
	for _, p := range points {
		d := p.Sub(s.Center)
		if sq := d[0]*d[0] + d[1]*d[1] + d[2]*d[2]; sq > maxSq {
			maxSq = sq
		}
	}
	s.Radius = float32(math.Sqrt(float64(maxSq)))
	return s
}
