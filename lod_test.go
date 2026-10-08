package pix_test

import (
	"testing"

	"github.com/bluescreen10/pix"
	"github.com/bluescreen10/pix/colors"
	"github.com/bluescreen10/pix/glm"
	"github.com/bluescreen10/pix/scenes"
)

// avgColor returns the mean of every non-background (non-black) pixel in the frame —
// enough to tell which of two very differently colored LOD levels rendered.
func avgColor(px []byte, size int) (r, g, b float64, lit int) {
	for i := 0; i+3 < len(px); i += 4 {
		rr, gg, bb := px[i], px[i+1], px[i+2]
		if rr == 0 && gg == 0 && bb == 0 {
			continue
		}
		r += float64(rr)
		g += float64(gg)
		b += float64(bb)
		lit++
	}
	if lit == 0 {
		return 0, 0, 0, 0
	}
	return r / float64(lit), g / float64(lit), b / float64(lit), lit
}

// TestMeshLODSelection renders a single LOD-enabled Mesh (red level 0, blue level 1,
// threshold at distance 10) at a near and a far camera distance, and checks the
// expected color dominates each frame — proof that scene_cull.comp's distance test
// (not just the frustum test) is actually gating which level's drawable survives.
func TestMeshLODSelection(t *testing.T) {
	const size = 128
	r, err := pix.NewOffscreenRenderer(size, size)
	if err != nil {
		t.Fatal(err)
	}
	defer r.Destroy()
	scene := scenes.New()
	defer scene.Destroy()
	scene.SetAmbient(colors.RGB32F{0.9, 0.9, 0.9}, 1)

	near := r.GeometryStore.Create(pix.BoxGeometry(1, 1, 1))
	far := r.GeometryStore.Create(pix.BoxGeometry(1, 1, 1))
	redMat := r.NewBasicMaterial()
	redMat.SetColor(colors.RGBA32F{1, 0, 0, 1})
	blueMat := r.NewBasicMaterial()
	blueMat.SetColor(colors.RGBA32F{0, 0, 1, 1})

	mesh := scene.NewMesh(near, redMat)
	mesh.AddLOD(far, blueMat, 10)

	cam := scene.NewPerspectiveCamera(45, 1, 0.05, 1000)
	cam.SetPosition(glm.Vec3f{0, 0, 5}) // within level 0's [0, 10) range
	cam.LookAt(glm.Vec3f{0, 0, 0})
	r.Render(scene)
	rr, gg, bb, lit := avgColor(r.Pixels(), size)
	t.Logf("near: avg=(%.0f,%.0f,%.0f) lit=%d", rr, gg, bb, lit)
	if lit < 100 || rr < 150 || bb > 50 {
		t.Fatalf("near camera should show the red (level 0) box, got avg=(%.0f,%.0f,%.0f) lit=%d", rr, gg, bb, lit)
	}

	cam.SetPosition(glm.Vec3f{0, 0, 20}) // past the threshold
	cam.LookAt(glm.Vec3f{0, 0, 0})
	r.Render(scene)
	rr, gg, bb, lit = avgColor(r.Pixels(), size)
	t.Logf("far: avg=(%.0f,%.0f,%.0f) lit=%d", rr, gg, bb, lit)
	if lit < 20 || bb < 150 || rr > 50 {
		t.Fatalf("far camera should show the blue (level 1) box, got avg=(%.0f,%.0f,%.0f) lit=%d", rr, gg, bb, lit)
	}
}

// TestInstancedMeshLODSelection is TestMeshLODSelection's InstancedMesh counterpart:
// two instances placed at very different distances from the camera, sharing one LOD
// group, should independently select their own level — proof that per-instance LOD
// selection actually works, not just per-Mesh.
func TestInstancedMeshLODSelection(t *testing.T) {
	const size = 128
	r, err := pix.NewOffscreenRenderer(size, size)
	if err != nil {
		t.Fatal(err)
	}
	defer r.Destroy()
	scene := scenes.New()
	defer scene.Destroy()
	scene.SetAmbient(colors.RGB32F{0.9, 0.9, 0.9}, 1)

	near := r.GeometryStore.Create(pix.BoxGeometry(1, 1, 1))
	far := r.GeometryStore.Create(pix.BoxGeometry(1, 1, 1))
	redMat := r.NewBasicMaterial()
	redMat.SetColor(colors.RGBA32F{1, 0, 0, 1})
	blueMat := r.NewBasicMaterial()
	blueMat.SetColor(colors.RGBA32F{0, 0, 1, 1})

	transforms := []glm.Mat4f{
		glm.Transform(glm.Vec3f{1, 1, 1}, glm.QuatfIdentity, glm.Vec3f{-1, 0, 0}),
		glm.Transform(glm.Vec3f{1, 1, 1}, glm.QuatfIdentity, glm.Vec3f{1, 0, 0}),
	}
	field := scene.NewInstancedMesh(near, redMat, transforms)
	field.AddLOD(far, blueMat, 10)

	// Camera far enough that BOTH instances are past the threshold: both should be blue.
	cam := scene.NewPerspectiveCamera(60, 1, 0.05, 1000)
	cam.SetPosition(glm.Vec3f{0, 0, 20})
	cam.LookAt(glm.Vec3f{0, 0, 0})
	r.Render(scene)
	rr, _, bb, lit := avgColor(r.Pixels(), size)
	t.Logf("both far: avg=(%.0f,_,%.0f) lit=%d", rr, bb, lit)
	if lit < 20 || bb < 150 || rr > 50 {
		t.Fatalf("both instances should show level 1 (blue) when far, got avg=(%.0f,_,%.0f)", rr, bb)
	}

	// Camera close enough that both are within the near threshold: both should be red.
	cam.SetPosition(glm.Vec3f{0, 0, 4})
	r.Render(scene)
	rr, _, bb, lit = avgColor(r.Pixels(), size)
	t.Logf("both near: avg=(%.0f,_,%.0f) lit=%d", rr, bb, lit)
	if lit < 20 || rr < 150 || bb > 50 {
		t.Fatalf("both instances should show level 0 (red) when near, got avg=(%.0f,_,%.0f)", rr, bb)
	}
}

// TestMeshLODHysteresis checks that a large hysteresis band keeps the currently
// selected level shown across a camera distance that crosses the raw boundary, as
// long as it doesn't clear the widened band — and that it still switches once the
// widened band is actually cleared.
func TestMeshLODHysteresis(t *testing.T) {
	const size = 128
	r, err := pix.NewOffscreenRenderer(size, size)
	if err != nil {
		t.Fatal(err)
	}
	defer r.Destroy()
	scene := scenes.New()
	defer scene.Destroy()
	scene.SetAmbient(colors.RGB32F{0.9, 0.9, 0.9}, 1)

	near := r.GeometryStore.Create(pix.BoxGeometry(1, 1, 1))
	far := r.GeometryStore.Create(pix.BoxGeometry(1, 1, 1))
	redMat := r.NewBasicMaterial()
	redMat.SetColor(colors.RGBA32F{1, 0, 0, 1})
	blueMat := r.NewBasicMaterial()
	blueMat.SetColor(colors.RGBA32F{0, 0, 1, 1})

	mesh := scene.NewMesh(near, redMat)
	mesh.AddLOD(far, blueMat, 10)
	mesh.SetLODHysteresis(5) // widened band: level 0 sticky up to 15, level 1 down to 5

	cam := scene.NewPerspectiveCamera(45, 1, 0.05, 1000) // looks down -Z, at the origin from every position below

	// First frame at distance 5: no prior selection, so it picks level 0 normally.
	cam.SetPosition(glm.Vec3f{0, 0, 5})
	r.Render(scene)
	_, _, bb, _ := avgColor(r.Pixels(), size)
	if bb > 50 {
		t.Fatalf("expected level 0 (red) at distance 5, got blue avg=%.0f", bb)
	}

	// Move just past the raw boundary (distance 12) but still inside the widened
	// sticky band (up to 15): should still show level 0.
	cam.SetPosition(glm.Vec3f{0, 0, 12})
	r.Render(scene)
	rr, _, bb, lit := avgColor(r.Pixels(), size)
	t.Logf("distance 12 (inside widened band): avg=(%.0f,_,%.0f) lit=%d", rr, bb, lit)
	if rr < 150 || bb > 50 {
		t.Fatalf("hysteresis should keep level 0 (red) at distance 12, got avg=(%.0f,_,%.0f)", rr, bb)
	}

	// Move well past the widened band: should now switch to level 1.
	cam.SetPosition(glm.Vec3f{0, 0, 20})
	r.Render(scene)
	rr, _, bb, lit = avgColor(r.Pixels(), size)
	t.Logf("distance 20 (past widened band): avg=(%.0f,_,%.0f) lit=%d", rr, bb, lit)
	if bb < 150 || rr > 50 {
		t.Fatalf("expected level 1 (blue) at distance 20, got avg=(%.0f,_,%.0f)", rr, bb)
	}

	// Now approach from far away: move to distance 7, inside level 1's OWN widened
	// band (down to 10-5=5) even though it's also inside level 0's un-widened band
	// ([0,10)). This is the direction that exposed a real bug: an ascending
	// level-by-level scan matches level 0's un-widened band before ever reaching
	// level 1's widened near edge, since adjacent levels' un-widened bands share a
	// boundary — so widening only ever took effect on a level's far edge, never its
	// near edge, for any level above 0. Fixed by checking prevLevel's own widened
	// band first, before the plain ascending scan (see scene_cull.comp's
	// selectLevel). Should still show level 1 (blue) here, not snap back to level 0.
	cam.SetPosition(glm.Vec3f{0, 0, 7})
	r.Render(scene)
	rr, _, bb, lit = avgColor(r.Pixels(), size)
	t.Logf("distance 7, approaching from far (inside level 1's widened near edge): avg=(%.0f,_,%.0f) lit=%d", rr, bb, lit)
	if bb < 150 || rr > 50 {
		t.Fatalf("hysteresis should keep level 1 (blue) at distance 7 when approaching from far, got avg=(%.0f,_,%.0f)", rr, bb)
	}

	// Move well inside level 0's territory, past even the widened band: should
	// finally drop back to level 0.
	cam.SetPosition(glm.Vec3f{0, 0, 3})
	r.Render(scene)
	rr, _, bb, lit = avgColor(r.Pixels(), size)
	t.Logf("distance 3 (past level 1's widened band): avg=(%.0f,_,%.0f) lit=%d", rr, bb, lit)
	if rr < 150 || bb > 50 {
		t.Fatalf("expected level 0 (red) at distance 3, got avg=(%.0f,_,%.0f)", rr, bb)
	}
}
