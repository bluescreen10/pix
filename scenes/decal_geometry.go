package scenes

import (
	"github.com/bluescreen10/pix/geometries"
	"github.com/bluescreen10/pix/glm"
	"github.com/chewxy/math32"
)

// decalNormalCutoff rejects source triangles whose facing deviates too far from
// the decal's projection axis (+Z in decal space). Without it, a box deep enough
// to reach the far side of a target also catches the surfaces perpendicular to
// the projection — the "smear down the wall" artifact every screen-space decal
// implementation guards against with the same test. cos(75°): generous enough to
// wrap a rounded corner, tight enough to drop a wall the decal only grazes.
const decalNormalCutoff = 0.2588

// decalOffsetFraction scales the outward push applied to every emitted vertex,
// relative to the decal box's own depth. The patch is coplanar with the surface
// it was clipped from, so without this it z-fights; with it, the patch sits a
// hair in front and its (blended, non-depth-writing) draw passes the depth test
// cleanly. Expressed as a fraction of size.z rather than an absolute distance so
// it behaves the same on a 1-unit crate and a 400-unit box.
const decalOffsetFraction = 0.001

// decalClipVertex is one polygon corner during clipping: a position in decal
// space plus the source attributes that have to be interpolated along with it
// when a clip plane splits an edge.
type decalClipVertex struct {
	pos    glm.Vec3f // decal space
	normal glm.Vec3f // decal space, not renormalized until emit
}

// lerp interpolates every field toward b by t.
func (v decalClipVertex) lerp(b decalClipVertex, t float32) decalClipVertex {
	return decalClipVertex{
		pos:    v.pos.Add(b.pos.Sub(v.pos).Scale(t)),
		normal: v.normal.Add(b.normal.Sub(v.normal).Scale(t)),
	}
}

// DecalGeometry clips mesh's triangles against a box and returns the conforming
// patch — a decal as real geometry rather than a screen-space projection, so it
// paints exactly the target it was clipped from and nothing else, with no extra
// render pass and no per-pixel depth reconstruction (see docs/decal-system.md).
//
// pos, orientation, and size are world-space: the box is centered at pos,
// oriented by orientation, and extends size/2 along each of its own axes, with
// the decal's texture projected down its local -Z (so +Z faces back out toward
// where a projector would sit). The returned geometry is in mesh's LOCAL space,
// which is what lets a caller parent it under mesh and have it follow the target
// for free:
//
//	geo := r.NewDecalGeometry(box, pos, rot, glm.Vec3f{350, 350, 300})
//	decal := scene.NewMesh(geo, decalMat)
//	decal.SetCastShadow(false)
//	box.Add(decal)
//
// DecalGeometry builds the clipped patch for NewDecalGeometry without uploading
// it, matching the plain-config half of every other builder in primitives.go.
// The bool reports whether anything survived clipping.
func DecalGeometry(mesh Mesh, pos glm.Vec3f, orientation glm.Quatf, size glm.Vec3f) (geometries.GeometryConfig, bool) {
	if !finiteDecalSize(size) {
		panic("pix: DecalGeometry requires finite, positive size components")
	}
	// Clipping reads mesh.WorldTransform(), which is only current as of the last
	// Scene.Sync() — and callers building a decal at scene-setup time, before the
	// first Render, haven't triggered one yet. Force it here rather than trust the
	// caller's timing: the alternative is a silent, hard-to-diagnose "no patch"
	// when a freshly SetPosition'd/parented target hasn't been synced. Sync()
	// itself is heavier than needed (skinning, the light table) — this is just its
	// transform-update half, safe to call early since both steps are no-ops when
	// nothing is dirty.
	sc := mesh.Scene()
	sc.flushTopoIfDirty()
	sc.updateTransforms()

	geo := mesh.Geometry()
	srcPos := geo.AttributeData[glm.Vec3f](geometries.AttributePosition)
	srcIdx := geo.Indices()
	if len(srcPos) == 0 || len(srcIdx) < 3 {
		return geometries.GeometryConfig{}, false
	}
	srcNormal := geo.AttributeData[glm.Vec3f](geometries.AttributeNormal)

	// Clip in decal space, where the box is axis-aligned and centered on the
	// origin: one matrix per vertex, and six trivial half-space tests instead of
	// six general plane equations. decalToWorld has unit scale on purpose — size
	// stays as half-extents rather than being baked in, so the clip bounds and
	// the UV divisor remain the caller's numbers.
	decalToWorld := glm.Transform(glm.Vec3f{1, 1, 1}, orientation, pos)
	meshLocalToDecal := decalToWorld.Inv().Mul4x4(mesh.WorldTransform())
	decalToMeshLocal := meshLocalToDecal.Inv()
	// Normals transform by the inverse-transpose of their direction's matrix, not
	// the matrix itself — identical for a pure rotation, but not once the target
	// carries non-uniform scale, which a scene node freely can.
	normalToDecal := decalToMeshLocal.Mat3().Transpose()
	normalToMeshLocal := meshLocalToDecal.Mat3().Transpose()
	half := glm.Vec3f{size[0] / 2, size[1] / 2, size[2] / 2}

	var (
		outPos  []glm.Vec3f
		outNrm  []glm.Vec3f
		outUV   []glm.Vec2f
		outIdx  []uint32
		poly    []decalClipVertex
		scratch []decalClipVertex
	)
	offset := size[2] * decalOffsetFraction

	for i := 0; i+2 < len(srcIdx); i += 3 {
		i0, i1, i2 := srcIdx[i], srcIdx[i+1], srcIdx[i+2]
		if int(i0) >= len(srcPos) || int(i1) >= len(srcPos) || int(i2) >= len(srcPos) {
			continue
		}
		p0 := decalSpacePoint(meshLocalToDecal, srcPos[i0])
		p1 := decalSpacePoint(meshLocalToDecal, srcPos[i1])
		p2 := decalSpacePoint(meshLocalToDecal, srcPos[i2])

		if outsideSamePlane(p0, p1, p2, half) {
			continue
		}
		face := p1.Sub(p0).Cross(p2.Sub(p0))
		if face.Length() == 0 {
			continue // degenerate triangle
		}
		n0 := decalSpaceNormal(normalToDecal, srcNormal, i0, face)
		n1 := decalSpaceNormal(normalToDecal, srcNormal, i1, face)
		n2 := decalSpaceNormal(normalToDecal, srcNormal, i2, face)

		// Facing test against the projection axis (+Z in decal space). This
		// averages the three shading normals rather than using the geometric
		// normal directly: authored normals point outward unambiguously, whereas
		// the cross product's sign depends on the source's winding convention,
		// which varies by asset and would silently put the patch on the far face
		// if it disagreed. Geometry with no normals falls back to the geometric
		// one anyway (see decalSpaceNormal).
		facing := n0.Add(n1).Add(n2)
		if l := facing.Length(); l == 0 || facing.Scale(1 / l)[2] < decalNormalCutoff {
			continue
		}

		poly = poly[:0]
		poly = append(poly,
			decalClipVertex{pos: p0, normal: n0},
			decalClipVertex{pos: p1, normal: n1},
			decalClipVertex{pos: p2, normal: n2},
		)
		poly = clipToBox(poly, &scratch, half)
		if len(poly) < 3 {
			continue
		}

		// Fan-triangulate the clipped polygon (convex by construction: a triangle
		// intersected with a box).
		base := uint32(len(outPos))
		for _, v := range poly {
			n := v.normal
			if l := n.Length(); l > 0 {
				n = n.Scale(1 / l)
			} else {
				n = glm.Vec3f{0, 0, 1}
			}
			// Push out along the normal before leaving decal space, so the offset
			// follows the surface rather than the projection direction.
			p := v.pos.Add(n.Scale(offset))
			outPos = append(outPos, decalSpacePoint(decalToMeshLocal, p))
			outNrm = append(outNrm, normalToMeshLocal.MulVec3(n).Normalize())
			// Same UV convention the screen-space decal shader used, so existing
			// decal textures keep their framing and orientation.
			outUV = append(outUV, glm.Vec2f{v.pos[0]/size[0] + 0.5, v.pos[1]/size[1] + 0.5})
		}
		for k := 1; k+1 < len(poly); k++ {
			outIdx = append(outIdx, base, base+uint32(k), base+uint32(k)+1)
		}
	}

	if len(outPos) == 0 {
		return geometries.GeometryConfig{}, false
	}
	return geometries.GeometryConfig{
		Attributes: []geometries.Attribute{
			geometries.NewAttribute(geometries.AttributePosition, geometries.Float32x3, outPos),
			geometries.NewAttribute(geometries.AttributeNormal, geometries.Float32x3, outNrm),
			geometries.NewAttribute(geometries.AttributeUV, geometries.Float32x2, outUV),
		},
		Indices: outIdx,
	}, true
}

// clipToBox clips a convex polygon against the six half-spaces of the box, using
// scratch as the alternating output buffer so a whole build reuses two slices
// rather than allocating per triangle. Standard Sutherland-Hodgman: for each
// plane, walk the edges and keep inside vertices, emitting an interpolated
// vertex wherever an edge crosses.
func clipToBox(poly []decalClipVertex, scratch *[]decalClipVertex, half glm.Vec3f) []decalClipVertex {
	for axis := 0; axis < 3 && len(poly) >= 3; axis++ {
		for _, positive := range [2]bool{true, false} {
			limit := half[axis]
			// distance > 0 means inside, for both the +limit and -limit planes.
			distance := func(v decalClipVertex) float32 {
				if positive {
					return limit - v.pos[axis]
				}
				return v.pos[axis] + limit
			}

			out := (*scratch)[:0]
			for i := range poly {
				cur := poly[i]
				prev := poly[(i+len(poly)-1)%len(poly)]
				dc, dp := distance(cur), distance(prev)
				if dc >= 0 {
					if dp < 0 {
						out = append(out, prev.lerp(cur, dp/(dp-dc)))
					}
					out = append(out, cur)
				} else if dp >= 0 {
					out = append(out, prev.lerp(cur, dp/(dp-dc)))
				}
			}
			// Swap the buffers: this pass's output is the next pass's input, and
			// the slice we just consumed becomes the scratch space.
			poly, *scratch = out, poly
			if len(poly) < 3 {
				return poly[:0]
			}
		}
	}
	return poly
}

// outsideSamePlane is the cheap trivial-reject: a triangle entirely beyond any
// single box plane cannot intersect the box, and most of a mesh's triangles fail
// here without ever being clipped.
func outsideSamePlane(p0, p1, p2, half glm.Vec3f) bool {
	for axis := 0; axis < 3; axis++ {
		if p0[axis] > half[axis] && p1[axis] > half[axis] && p2[axis] > half[axis] {
			return true
		}
		if p0[axis] < -half[axis] && p1[axis] < -half[axis] && p2[axis] < -half[axis] {
			return true
		}
	}
	return false
}

// decalSpacePoint transforms a point (w = 1, unlike glm.Vec3f.Vec4's direction
// conversion, which leaves w at zero).
func decalSpacePoint(m glm.Mat4f, p glm.Vec3f) glm.Vec3f {
	v := m.Mul4x1(glm.Vec4f{p[0], p[1], p[2], 1})
	return glm.Vec3f{v[0], v[1], v[2]}
}

// decalSpaceNormal returns vertex i's shading normal in decal space, falling back
// to the triangle's geometric normal when the source geometry carries no normals
// at all (flat shading, which is the honest result for geometry that never had
// them).
func decalSpaceNormal(m glm.Mat3[float32], normals []glm.Vec3f, i uint32, face glm.Vec3f) glm.Vec3f {
	if int(i) < len(normals) {
		return m.MulVec3(normals[i])
	}
	return face
}

func finiteDecalSize(v glm.Vec3f) bool {
	for _, c := range v {
		if c <= 0 || math32.IsNaN(c) || math32.IsInf(c, 0) {
			return false
		}
	}
	return true
}
