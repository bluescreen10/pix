package pix

import (
	"math"
	"testing"

	"github.com/bluescreen10/pix/cameras"
	"github.com/bluescreen10/pix/colors"
	"github.com/bluescreen10/pix/geometries"
	"github.com/bluescreen10/pix/glm"
	"github.com/bluescreen10/pix/materials"
	"github.com/bluescreen10/pix/scenes"
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

// patchOf clips and returns the raw patch data, failing the test if nothing
// survived.
func patchOf(t *testing.T, box scenes.Mesh, pos glm.Vec3f, rot glm.Quatf, size glm.Vec3f) ([]glm.Vec3f, []glm.Vec2f, []uint32) {
	t.Helper()
	cfg, ok := scenes.DecalGeometry(box, pos, rot, size)
	if !ok {
		t.Fatal("scenes.DecalGeometry produced no patch, expected one")
	}
	var pos3 []glm.Vec3f
	var uvs []glm.Vec2f
	for _, a := range cfg.Attributes {
		switch a.Type() {
		case geometries.AttributePosition:
			pos3 = geometries.AttributeData[glm.Vec3f](a)
		case geometries.AttributeUV:
			uvs = geometries.AttributeData[glm.Vec2f](a)
		}
	}
	if len(pos3) == 0 {
		t.Fatal("patch has no positions")
	}
	return pos3, uvs, cfg.Indices
}

// TestDecalGeometryClipsToBox is the core clipping check: a decal box smaller
// than the face it projects onto must produce a patch confined to that box's
// footprint, sitting on the face it was aimed at.
func TestDecalGeometryClipsToBox(t *testing.T) {
	_, _, box := decalTestScene(t, 32, 32, 2) // box spans [-1,1] on every axis

	// Aimed at the +Z face from outside, covering the middle half of it.
	pos, uvs, idx := patchOf(t, box, glm.Vec3f{0, 0, 1}, glm.QuatIdentityf, glm.Vec3f{1, 1, 1})

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
	pos, uvs, _ := patchOf(t, box, glm.Vec3f{0, 0, 1}, glm.QuatIdentityf, glm.Vec3f{1, 1, 1})

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
	pos, _, _ := patchOf(t, box, glm.Vec3f{1, 0, 1}, glm.QuatIdentityf, glm.Vec3f{1, 1, 1})

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

	if _, ok := scenes.DecalGeometry(box, glm.Vec3f{50, 50, 50}, glm.QuatIdentityf, glm.Vec3f{1, 1, 1}); ok {
		t.Error("scenes.DecalGeometry reported a patch for a box that misses the mesh entirely")
	}
	geo := r.NewDecalGeometry(box, glm.Vec3f{50, 50, 50}, glm.QuatIdentityf, glm.Vec3f{1, 1, 1})
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
	pos, _, _ := patchOf(t, box, glm.Vec3f{0, 0, 0}, glm.QuatIdentityf, glm.Vec3f{4, 4, 4})
	for i, p := range pos {
		if p[2] < 0.9 {
			t.Errorf("vertex %d at %v came from a face other than +Z", i, p)
		}
	}
}

// TestDecalGeometryTargetsOneObject is what the object-ID buffer used to exist
// for, now structural: clipping against one box cannot produce geometry touching
// a second box beside it, whatever the projection volume covers.
func TestDecalGeometryTargetsOneObject(t *testing.T) {
	r, scene, boxA := decalTestScene(t, 64, 64, 2)
	boxA.SetPosition(glm.Vec3f{-2, 0, 0})

	geo := r.GeometryStore.Create(BoxGeometry(2, 2, 2))
	defer geo.Release()
	mat := r.NewBasicMaterial()
	defer mat.Release()
	boxB := scene.NewMesh(geo, mat)
	boxB.SetPosition(glm.Vec3f{2, 0, 0})
	scene.Add(boxB)
	// Deliberately no Scene.Sync()/Render() here: this also covers the case that
	// broke the beach example — DecalGeometry must see boxA's just-set position
	// even though nothing has synced the scene's world transforms yet.

	// A projection volume wide enough to span both boxes, clipped against boxA.
	cfg, ok := scenes.DecalGeometry(boxA, glm.Vec3f{0, 0, 1}, glm.QuatIdentityf, glm.Vec3f{8, 2, 4})
	if !ok {
		t.Fatal("expected a patch on boxA")
	}
	var pos []glm.Vec3f
	for _, a := range cfg.Attributes {
		if a.Type() == geometries.AttributePosition {
			pos = geometries.AttributeData[glm.Vec3f](a)
		}
	}
	// Patch vertices are in boxA's local space; boxB sits 4 units away in world
	// space, so nothing may reach it.
	for i, p := range pos {
		world := p.Add(glm.Vec3f{-2, 0, 0})
		if world[0] > 0 {
			t.Errorf("vertex %d reaches world x=%.2f, crossing into boxB's half", i, world[0])
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

	geo := r.NewDecalGeometry(box, glm.Vec3f{0, 0, 1}, glm.QuatIdentityf, glm.Vec3f{1, 1, 1})
	if !geo.IsValid() {
		t.Fatal("expected a decal patch")
	}
	defer geo.Release()
	decal := scene.NewMesh(geo, decalMat)
	decal.SetCastShadow(false)
	box.Add(decal)

	cam := cameras.NewOrthographicCamera(-1, 1, -1, 1, 0.1, 100)
	cam.SetPosition(glm.Vec3f{0, 0, 10})
	cam.SetTarget(glm.Vec3f{0, 0, 0})
	r.Render(scene, cam)

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
