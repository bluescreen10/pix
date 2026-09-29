package pix

import (
	"github.com/bluescreen10/pix/colors"
	"github.com/bluescreen10/pix/geometries"
	"github.com/bluescreen10/pix/glm"
	"github.com/bluescreen10/pix/materials"
	"github.com/bluescreen10/pix/scenes"
	"math"
	"testing"
)

// decalTestScene builds a renderer + scene with one unit-ish box at the origin,
// the shape every clipping test below projects onto.
func decalTestScene(t *testing.T, w, h uint32, size float32) (*Renderer, *scenes.Scene, scenes.Mesh) {
	t.Helper()
	r, err := NewOffscreenRenderer(w, h)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(r.Destroy)

	scene := scenes.New()
	t.Cleanup(scene.Destroy)

	geo := r.GeometryStore.Create(BoxGeometry(size, size, size))
	defer geo.Release()
	mat := r.NewBasicMaterial()
	mat.SetColor(colors.RGBA32F{1, 1, 1, 1})
	defer mat.Release()

	box := scene.NewMesh(geo, mat)
	scene.Add(box)
	return r, scene, box
}

// patchOf clips and returns the uploaded patch data, failing the test if nothing
// survived.
func patchOf(t *testing.T, source geometries.Geometry, position glm.Vec3f, orientation glm.Quatf, size glm.Vec3f) ([]glm.Vec3f, []glm.Vec2f, []uint32) {
	t.Helper()
	patch := source.Clip(position, orientation, size)
	if !patch.IsValid() {
		t.Fatal("Geometry.Clip produced no patch, expected one")
	}
	t.Cleanup(patch.Release)
	positions := patch.AttributeData[glm.Vec3f](geometries.AttributePosition)
	uvs := patch.AttributeData[glm.Vec2f](geometries.AttributeUV)
	if len(positions) == 0 {
		t.Fatal("patch has no positions")
	}
	return positions, uvs, patch.Indices()
}

// TestDecalGeometryClipsToBox is the core clipping check: a decal box smaller
// than the face it projects onto must produce a patch confined to that box's
// footprint, sitting on the face it was aimed at.
func TestDecalGeometryClipsToBox(t *testing.T) {
	_, _, box := decalTestScene(t, 32, 32, 2) // box spans [-1,1] on every axis

	// Aimed at the +Z face from outside, covering the middle half of it.
	pos, uvs, idx := patchOf(t, box.Geometry(), glm.Vec3f{0, 0, 1}, glm.QuatfIdentity, glm.Vec3f{1, 1, 1})

	if len(idx)%3 != 0 || len(idx) == 0 {
		t.Fatalf("index count %d is not a positive multiple of 3", len(idx))
	}
	for i, p := range pos {
		// Confined to the decal box's X/Y footprint (half-extent 0.5), with a
		// little slack for the outward offset.
		if math.Abs(float64(p[0])) > 0.5+1e-3 || math.Abs(float64(p[1])) > 0.5+1e-3 {
			t.Errorf("vertex %d at %v escapes the decal box's XY footprint", i, p)
		}
		// On the +Z face, not the far one: the facing test must have rejected
		// the -Z face even though the box's depth reaches it.
		if p[2] < 0.9 {
			t.Errorf("vertex %d at %v is not on the +Z face (facing test failed?)", i, p)
		}
	}
	for i, uv := range uvs {
		if uv[0] < -1e-3 || uv[0] > 1+1e-3 || uv[1] < -1e-3 || uv[1] > 1+1e-3 {
			t.Errorf("uv %d = %v outside [0,1]", i, uv)
		}
	}
}

// TestDecalGeometryUVConvention pins the UV mapping the old screen-space shader
// used (uv = local.xy/size + 0.5), so existing decal textures keep their framing:
// the decal box's center must land at uv (0.5, 0.5).
func TestDecalGeometryUVConvention(t *testing.T) {
	_, _, box := decalTestScene(t, 32, 32, 2)
	pos, uvs, _ := patchOf(t, box.Geometry(), glm.Vec3f{0, 0, 1}, glm.QuatfIdentity, glm.Vec3f{1, 1, 1})

	// The patch covers the decal box exactly, so its corners are the uv corners
	// and its center is (0.5, 0.5). Check the correspondence pointwise instead of
	// hunting for a center vertex: uv must track x/y linearly.
	for i := range pos {
		wantU := pos[i][0]/1 + 0.5
		wantV := pos[i][1]/1 + 0.5
		if math.Abs(float64(uvs[i][0]-wantU)) > 1e-3 || math.Abs(float64(uvs[i][1]-wantV)) > 1e-3 {
			t.Fatalf("vertex %d at %v has uv %v, want (%.3f, %.3f)", i, pos[i], uvs[i], wantU, wantV)
		}
	}
}

// TestDecalGeometryStraddlesEdge checks the clip against a box positioned to hang
// off the face's edge: the patch must be cut at the decal box's bound, never
// extending past it into thin air.
func TestDecalGeometryStraddlesEdge(t *testing.T) {
	_, _, box := decalTestScene(t, 32, 32, 2)
	// Centered on the face's +X edge, so half the decal box hangs off the target.
	pos, _, _ := patchOf(t, box.Geometry(), glm.Vec3f{1, 0, 1}, glm.QuatfIdentity, glm.Vec3f{1, 1, 1})

	for i, p := range pos {
		// Cut at the target's own edge (x <= 1) and at the decal box's far bound.
		if p[0] > 1+1e-3 {
			t.Errorf("vertex %d at %v extends past the target's +X edge", i, p)
		}
		if p[0] < 0.5-1e-3 {
			t.Errorf("vertex %d at %v extends past the decal box's -X bound", i, p)
		}
	}
}

// TestDecalGeometryMisses covers the empty result: a box nowhere near the mesh
// must report "no patch" rather than panicking or returning a broken geometry.
func TestDecalGeometryMisses(t *testing.T) {
	r, _, box := decalTestScene(t, 32, 32, 2)

	geo := r.NewDecalGeometry(box.Geometry(), glm.Vec3f{50, 50, 50}, glm.QuatfIdentity, glm.Vec3f{1, 1, 1})
	if geo.IsValid() {
		t.Error("NewDecalGeometry returned a valid geometry for a box that misses")
	}
}

// TestDecalGeometryRejectsBackFaces is the normal-rejection check: a decal aimed
// along +Z must not pick up the box's side faces, even though a deep projection
// box contains them. This is the "smear down the wall" case screen-space decals
// could not exclude without an object-ID test.
func TestDecalGeometryRejectsBackFaces(t *testing.T) {
	_, _, box := decalTestScene(t, 32, 32, 2)

	// A box deep and wide enough to swallow the entire target.
	pos, _, _ := patchOf(t, box.Geometry(), glm.Vec3f{0, 0, 0}, glm.QuatfIdentity, glm.Vec3f{4, 4, 4})
	for i, p := range pos {
		if p[2] < 0.9 {
			t.Errorf("vertex %d at %v came from a face other than +Z", i, p)
		}
	}
}

// TestDecalGeometryUsesGeometryLocalSpace verifies that scene-node transforms do not
// affect clipping. The source handle and projector are both geometry-local.
func TestDecalGeometryUsesGeometryLocalSpace(t *testing.T) {
	_, _, box := decalTestScene(t, 32, 32, 2)
	box.SetPosition(glm.Vec3f{-20, 7, 13})

	positions, _, _ := patchOf(t, box.Geometry(), glm.Vec3f{0, 0, 1}, glm.QuatfIdentity, glm.Vec3f{1, 1, 1})
	for i, position := range positions {
		if math.Abs(float64(position[0])) > 0.5+1e-3 || math.Abs(float64(position[1])) > 0.5+1e-3 {
			t.Errorf("vertex %d at %v is not in geometry-local decal bounds", i, position)
		}
	}
}

// TestDecalGeometryRenders is the end-to-end check: the patch, given an
// alpha-blended material, actually paints its color over the target and nothing
// else — through the ordinary mesh path, with no decal-specific pass.
func TestDecalGeometryRenders(t *testing.T) {
	r, scene, box := decalTestScene(t, 64, 64, 2)
	r.SetClearColor(colors.RGBA32F{0, 0, 0, 1})

	decalMat := r.NewBasicMaterial()
	decalMat.SetColor(colors.RGBA32F{0, 1, 0, 1})
	decalMat.SetBlend(materials.BlendAlpha)
	defer decalMat.Release()

	geo := r.NewDecalGeometry(box.Geometry(), glm.Vec3f{0, 0, 1}, glm.QuatfIdentity, glm.Vec3f{1, 1, 1})
	if !geo.IsValid() {
		t.Fatal("expected a decal patch")
	}
	defer geo.Release()
	decal := scene.NewMesh(geo, decalMat)
	decal.SetCastShadow(false)
	box.Add(decal)

	cam := scene.NewOrthographicCamera(-1, 1, -1, 1, 0.1, 100)
	scene.Add(cam)
	cam.SetPosition(glm.Vec3f{0, 0, 10})
	cam.LookAt(glm.Vec3f{0, 0, 0})
	r.Render(scene)

	px := r.Pixels()
	// Center is inside the decal's footprint (half-extent 0.5 of a 2-unit face).
	cr, cg, cb, _ := samplePixel(px, 64, 32, 32)
	if cg < 200 || cr > 60 || cb > 60 {
		t.Errorf("center pixel = (%d,%d,%d), want green (decal patch drew)", cr, cg, cb)
	}
	// Near the face's corner, outside the decal but still on the box: the box's
	// own white, never the decal's green.
	er, eg, eb, _ := samplePixel(px, 64, 6, 32)
	if eg > 200 && er < 60 && eb < 60 {
		t.Errorf("edge pixel = (%d,%d,%d) is decal green, but lies outside the decal footprint", er, eg, eb)
	}
}

// samplePixel reads the RGBA8 pixel at (x, y) from an RGBA8 Pixels() buffer of the
// given width.
func samplePixel(px []byte, width, x, y int) (r, g, b, a byte) {
	i := (y*width + x) * 4
	return px[i], px[i+1], px[i+2], px[i+3]
}
