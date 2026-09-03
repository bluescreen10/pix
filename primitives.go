package pix

import (
	"github.com/bluescreen10/pix/glm"
	"github.com/chewxy/math32"
)

// Primitive geometry generators. Each returns a GeometryConfig (plain CPU data, no
// GPU resources) so it can be inspected or edited before upload; the matching
// Renderer.New*Geometry method is the one-line shorthand that uploads it.
//
// All of them wind triangles counter-clockwise when seen from outside, which is
// what the draw pipelines expect (they set FrontFaceCW because the renderer flips
// clip-space Y for Vulkan's Y-down NDC, reversing the apparent winding on screen).

// latheRow is one horizontal ring of a surface of revolution: the ring's height and
// radius, the normal's split into vertical and radial parts (already normalized
// against each other), and the texture coordinate down the surface. A radius of 0
// is a pole — its ring collapses to a point and the quads touching it degenerate
// into triangles, which is how sphere poles and disc centers are expressed.
type latheRow struct {
	y, radius             float32
	normalY, normalRadial float32
	v                     float32
}

// revolve sweeps rows around the Y axis in radialSegments steps, emitting a vertex
// grid of (radialSegments+1) × len(rows) — the seam column is duplicated so its UVs
// can run to 1 rather than wrapping to 0.
//
// This is the single place the winding convention lives: the radial direction is
// (-cos φ, 0, sin φ) and quads emit as (a, c, b), (c, d, b), which together put the
// front face outward. Every primitive below inherits that, so none of them can get
// it individually wrong.
func revolve(rows []latheRow, radialSegments int) GeometryConfig {
	if radialSegments < 3 {
		radialSegments = 3
	}
	cols := radialSegments + 1
	positions := make([]glm.Vec3f, 0, cols*len(rows))
	normals := make([]glm.Vec3f, 0, cols*len(rows))
	uvs := make([]glm.Vec2f, 0, cols*len(rows))

	for _, row := range rows {
		for ix := 0; ix < cols; ix++ {
			u := float32(ix) / float32(radialSegments)
			sinPhi, cosPhi := math32.Sincos(u * 2 * math32.Pi)
			positions = append(positions, glm.Vec3f{
				-row.radius * cosPhi,
				row.y,
				row.radius * sinPhi,
			})
			normals = append(normals, glm.Vec3f{
				-row.normalRadial * cosPhi,
				row.normalY,
				row.normalRadial * sinPhi,
			})
			uvs = append(uvs, glm.Vec2f{u, 1 - row.v})
		}
	}

	indices := make([]uint32, 0, radialSegments*(len(rows)-1)*6)
	for iy := 0; iy+1 < len(rows); iy++ {
		// Two rows at the same place are a hard edge, not a surface: they exist so
		// the seam can carry two different normals (a cylinder's cap rim vs. its
		// side). Spanning them would only emit zero-area triangles.
		if rows[iy].y == rows[iy+1].y && rows[iy].radius == rows[iy+1].radius {
			continue
		}
		for ix := 0; ix < radialSegments; ix++ {
			a := uint32(iy*cols + ix)
			b := a + 1
			c := uint32((iy+1)*cols + ix)
			d := c + 1
			// Skip the degenerate half of a quad that touches a pole, so a sphere
			// or a capped cylinder doesn't carry zero-area triangles.
			if rows[iy].radius > 0 {
				indices = append(indices, a, c, b)
			}
			if rows[iy+1].radius > 0 {
				indices = append(indices, c, d, b)
			}
		}
	}

	return GeometryConfig{
		Attributes: []Attribute{
			NewAttribute(AttributePosition, Float32x3, positions),
			NewAttribute(AttributeNormal, Float32x3, normals),
			NewAttribute(AttributeUV, Float32x2, uvs),
		},
		Indices: indices,
	}
}

// BoxGeometry is an axis-aligned box centered on the origin, with flat per-face
// normals (24 vertices — the corners are not shared, so each face shades flat).
func BoxGeometry(width, height, depth float32) GeometryConfig {
	hw, hh, hd := width/2, height/2, depth/2

	// One entry per face: its outward normal and its four corners, wound
	// counter-clockwise seen from outside.
	faces := []struct {
		normal  glm.Vec3f
		corners [4]glm.Vec3f
	}{
		{glm.Vec3f{1, 0, 0}, [4]glm.Vec3f{{hw, -hh, hd}, {hw, -hh, -hd}, {hw, hh, -hd}, {hw, hh, hd}}},
		{glm.Vec3f{-1, 0, 0}, [4]glm.Vec3f{{-hw, -hh, -hd}, {-hw, -hh, hd}, {-hw, hh, hd}, {-hw, hh, -hd}}},
		{glm.Vec3f{0, 1, 0}, [4]glm.Vec3f{{-hw, hh, hd}, {hw, hh, hd}, {hw, hh, -hd}, {-hw, hh, -hd}}},
		{glm.Vec3f{0, -1, 0}, [4]glm.Vec3f{{-hw, -hh, -hd}, {hw, -hh, -hd}, {hw, -hh, hd}, {-hw, -hh, hd}}},
		{glm.Vec3f{0, 0, 1}, [4]glm.Vec3f{{-hw, -hh, hd}, {hw, -hh, hd}, {hw, hh, hd}, {-hw, hh, hd}}},
		{glm.Vec3f{0, 0, -1}, [4]glm.Vec3f{{hw, -hh, -hd}, {-hw, -hh, -hd}, {-hw, hh, -hd}, {hw, hh, -hd}}},
	}

	positions := make([]glm.Vec3f, 0, 24)
	normals := make([]glm.Vec3f, 0, 24)
	uvs := make([]glm.Vec2f, 0, 24)
	indices := make([]uint32, 0, 36)
	for _, f := range faces {
		base := uint32(len(positions))
		positions = append(positions, f.corners[0], f.corners[1], f.corners[2], f.corners[3])
		normals = append(normals, f.normal, f.normal, f.normal, f.normal)
		uvs = append(uvs, glm.Vec2f{0, 0}, glm.Vec2f{1, 0}, glm.Vec2f{1, 1}, glm.Vec2f{0, 1})
		indices = append(indices, base, base+1, base+2, base, base+2, base+3)
	}

	return GeometryConfig{
		Attributes: []Attribute{
			NewAttribute(AttributePosition, Float32x3, positions),
			NewAttribute(AttributeNormal, Float32x3, normals),
			NewAttribute(AttributeUV, Float32x2, uvs),
		},
		Indices: indices,
	}
}

// PlaneGeometry is a flat grid in the XZ plane facing +Y — a ground plane, not
// three.js's XY orientation, so it needs no rotation to be a floor. Centered on the
// origin, subdivided widthSegments × depthSegments.
//
// Two triangles would represent a plane exactly; the subdivision is there for what
// consumes vertices rather than pixels — vertex displacement, terrain, or anything
// that wants interior vertices to move.
func PlaneGeometry(width, depth float32, widthSegments, depthSegments int) GeometryConfig {
	widthSegments = max(widthSegments, 1)
	depthSegments = max(depthSegments, 1)
	cols, rows := widthSegments+1, depthSegments+1
	hw, hd := width/2, depth/2

	positions := make([]glm.Vec3f, 0, cols*rows)
	normals := make([]glm.Vec3f, 0, cols*rows)
	uvs := make([]glm.Vec2f, 0, cols*rows)
	for ix := range cols {
		u := float32(ix) / float32(widthSegments)
		for iz := range rows {
			v := float32(iz) / float32(depthSegments)
			positions = append(positions, glm.Vec3f{-hw + u*width, 0, -hd + v*depth})
			normals = append(normals, glm.Vec3f{0, 1, 0})
			uvs = append(uvs, glm.Vec2f{u, v})
		}
	}

	indices := make([]uint32, 0, widthSegments*depthSegments*6)
	for ix := range widthSegments {
		for iz := range depthSegments {
			a := uint32(ix*rows + iz)
			b := a + 1
			c := uint32((ix+1)*rows + iz + 1)
			d := c - 1
			// Wound so the right-hand rule points +Y, matching the normal above.
			indices = append(indices, a, b, c, a, c, d)
		}
	}

	return GeometryConfig{
		Attributes: []Attribute{
			NewAttribute(AttributePosition, Float32x3, positions),
			NewAttribute(AttributeNormal, Float32x3, normals),
			NewAttribute(AttributeUV, Float32x2, uvs),
		},
		Indices: indices,
	}
}

// SphereGeometry is a UV sphere centered on the origin: widthSegments around the
// equator, heightSegments from pole to pole.
func SphereGeometry(radius float32, widthSegments, heightSegments int) GeometryConfig {
	if heightSegments < 2 {
		heightSegments = 2
	}
	rows := make([]latheRow, 0, heightSegments+1)
	for iy := 0; iy <= heightSegments; iy++ {
		v := float32(iy) / float32(heightSegments)
		sinTheta, cosTheta := math32.Sincos(v * math32.Pi)
		rows = append(rows, latheRow{
			y: radius * cosTheta, radius: radius * sinTheta,
			normalY: cosTheta, normalRadial: sinTheta,
			v: 1 - v, // iy 0 is the +Y pole, which is the top of the texture
		})
	}
	return revolve(rows, widthSegments)
}

// CylinderGeometry is a capped cylinder (or truncated cone, when the two radii
// differ) centered on the origin and running along Y. A radius of 0 at one end
// makes a cone.
func CylinderGeometry(radiusTop, radiusBottom, height float32, radialSegments, heightSegments int) GeometryConfig {
	if heightSegments < 1 {
		heightSegments = 1
	}
	half := height / 2
	// The side normal tilts by the cone's slope: straight out for a true cylinder,
	// increasingly upward as the top narrows.
	slope := (radiusBottom - radiusTop) / height
	sideLen := math32.Sqrt(1 + slope*slope)
	nRadial, nY := 1/sideLen, slope/sideLen

	rows := make([]latheRow, 0, heightSegments+5)
	// Top cap: a disc swept from the axis out to the rim, normal straight up.
	if radiusTop > 0 {
		rows = append(rows,
			latheRow{y: half, radius: 0, normalY: 1, v: 1},
			latheRow{y: half, radius: radiusTop, normalY: 1, v: 1},
		)
	}
	// Side.
	for i := 0; i <= heightSegments; i++ {
		t := float32(i) / float32(heightSegments)
		rows = append(rows, latheRow{
			y:       half - t*height,
			radius:  radiusTop + t*(radiusBottom-radiusTop),
			normalY: nY, normalRadial: nRadial,
			v: 1 - t,
		})
	}
	// Bottom cap, normal straight down.
	if radiusBottom > 0 {
		rows = append(rows,
			latheRow{y: -half, radius: radiusBottom, normalY: -1, v: 0},
			latheRow{y: -half, radius: 0, normalY: -1, v: 0},
		)
	}
	return revolve(rows, radialSegments)
}

// CapsuleGeometry is a cylinder of the given length along Y with a hemispherical
// cap of the given radius on each end, centered on the origin (so its total height
// is length + 2*radius). capSegments is the number of rings per hemisphere.
func CapsuleGeometry(radius, length float32, capSegments, radialSegments int) GeometryConfig {
	if capSegments < 1 {
		capSegments = 1
	}
	half := length / 2

	// One ring list from the top pole to the bottom pole. The last row of the top
	// cap and the first of the bottom cap are both the full-radius equator ring at
	// ±half, so the span between them is the cylindrical body — no separate side
	// section needed, and the surface stays watertight by construction.
	rows := make([]latheRow, 0, 2*(capSegments+1))
	for i := 0; i <= capSegments; i++ {
		phi := float32(i) / float32(capSegments) * (math32.Pi / 2)
		sinPhi, cosPhi := math32.Sincos(phi)
		rows = append(rows, latheRow{
			y: half + radius*cosPhi, radius: radius * sinPhi,
			normalY: cosPhi, normalRadial: sinPhi,
		})
	}
	for i := 0; i <= capSegments; i++ {
		phi := math32.Pi/2 + float32(i)/float32(capSegments)*(math32.Pi/2)
		sinPhi, cosPhi := math32.Sincos(phi)
		rows = append(rows, latheRow{
			y: -half + radius*cosPhi, radius: radius * sinPhi,
			normalY: cosPhi, normalRadial: sinPhi,
		})
	}
	// V runs top to bottom over the whole silhouette.
	for i := range rows {
		rows[i].v = 1 - float32(i)/float32(len(rows)-1)
	}
	return revolve(rows, radialSegments)
}

// NewBoxGeometry uploads a BoxGeometry and returns its handle.
func (r *Renderer) NewBoxGeometry(width, height, depth float32) Geometry {
	return r.NewGeometry(BoxGeometry(width, height, depth))
}

// NewPlaneGeometry uploads a PlaneGeometry and returns its handle.
func (r *Renderer) NewPlaneGeometry(width, depth float32, widthSegments, depthSegments int) Geometry {
	return r.NewGeometry(PlaneGeometry(width, depth, widthSegments, depthSegments))
}

// NewSphereGeometry uploads a SphereGeometry and returns its handle.
func (r *Renderer) NewSphereGeometry(radius float32, widthSegments, heightSegments int) Geometry {
	return r.NewGeometry(SphereGeometry(radius, widthSegments, heightSegments))
}

// NewCylinderGeometry uploads a CylinderGeometry and returns its handle.
func (r *Renderer) NewCylinderGeometry(radiusTop, radiusBottom, height float32, radialSegments, heightSegments int) Geometry {
	return r.NewGeometry(CylinderGeometry(radiusTop, radiusBottom, height, radialSegments, heightSegments))
}

// NewCapsuleGeometry uploads a CapsuleGeometry and returns its handle.
func (r *Renderer) NewCapsuleGeometry(radius, length float32, capSegments, radialSegments int) Geometry {
	return r.NewGeometry(CapsuleGeometry(radius, length, capSegments, radialSegments))
}
