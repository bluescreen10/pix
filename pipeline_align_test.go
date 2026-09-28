package pix_test

import (
	"bytes"
	"testing"

	"github.com/bluescreen10/pix"
	"github.com/bluescreen10/pix/cameras"
	"github.com/bluescreen10/pix/colors"
	"github.com/bluescreen10/pix/glm"
	"github.com/bluescreen10/pix/scenes"
)

// TestPipelineAlignmentDetachedMesh pins the parallel-array contract the renderer's
// batching relies on internally: a mesh created but never added to the scene (or later
// detached) must have exactly zero effect on how any other mesh renders.
//
// This used to be resolved by a second walk of the payload lists that did not filter
// the same way the real drawable-collection walk does, so an orphaned mesh shifted
// every following material index by one — not just a wrong shader, but a drawable
// reading another material type's record buffer at that index. Checked here by
// rendering the same attached mesh with and without an orphan mesh existing alongside
// it and requiring bit-identical output.
func TestPipelineAlignmentDetachedMesh(t *testing.T) {
	render := func(withOrphan bool) []byte {
		r, err := pix.NewOffscreenRenderer(64, 64)
		if err != nil {
			t.Fatal(err)
		}
		defer r.Destroy()
		scene := scenes.New()
		defer scene.Destroy()
		scene.SetAmbient(colors.RGB32F{0.8, 0.8, 0.8})

		geo := r.GeometryStore.Create(pix.BoxGeometry(1, 1, 1))
		phong := r.NewBlinnPhongMaterial()
		phong.SetColor(colors.RGBA32F{0.2, 0.6, 0.9, 1})

		if withOrphan {
			basic := r.NewBasicMaterial()
			scene.NewMesh(geo, basic) // created but never added to the scene root
		}

		added := scene.NewMesh(geo, phong)
		scene.Add(added)

		cam := cameras.NewPerspectiveCamera(45, 1, 0.1, 100)
		cam.SetPosition(glm.Vec3f{0, 0, 3})
		r.Render(scene, cam)
		return append([]byte(nil), r.Pixels()...)
	}

	without := render(false)
	with := render(true)
	if !bytes.Equal(without, with) {
		t.Fatal("an orphaned (never-added) mesh changed how the attached mesh rendered — " +
			"material/pipeline indices likely shifted out of alignment")
	}
}

// TestDrawableFlagsFollowShadowToggle covers Node.SetCastShadow: toggling it after the
// draw list was already built has to actually reach the GPU, or the renderer keeps
// casting (or not casting) a shadow the caller just asked to change. Checked by
// rendering an occluder over a receiver with a shadow-casting light and comparing
// scene brightness before and after the toggle.
func TestDrawableFlagsFollowShadowToggle(t *testing.T) {
	r, err := pix.NewOffscreenRenderer(160, 160)
	if err != nil {
		t.Fatal(err)
	}
	defer r.Destroy()
	r.EnableShadows(true)
	r.SetClearColor(colors.RGBA32F{0, 0, 0, 1})

	scene := scenes.New()
	defer scene.Destroy()
	scene.SetAmbient(colors.RGB32F{0.05, 0.05, 0.05})
	light := scene.AddDirectionalLight(glm.Vec3f{0.15, -1, 0.15}, colors.RGB32F{1, 1, 1}, 3)
	light.SetCastShadow(true)

	geo := r.GeometryStore.Create(pix.BoxGeometry(1, 1, 1))
	ground := scene.NewMesh(geo, r.NewPBRMaterial())
	ground.SetScale(glm.Vec3f{6, 0.2, 6})
	scene.Add(ground)

	occluder := scene.NewMesh(geo, r.NewPBRMaterial())
	occluder.SetPosition(glm.Vec3f{0, 1.5, 0})
	occluder.SetScale(glm.Vec3f{0.8, 0.8, 0.8})
	scene.Add(occluder)

	cam := cameras.NewPerspectiveCamera(45, 1, 0.1, 100)
	cam.SetPosition(glm.Vec3f{0, 5, 6})
	cam.SetTarget(glm.Vec3f{0, 0, 0})

	occluder.SetCastShadow(false)
	r.Render(scene, cam)
	notCasting := sceneLuma(r.Pixels())

	occluder.SetCastShadow(true)
	r.Render(scene, cam)
	casting := sceneLuma(r.Pixels())

	if casting >= notCasting {
		t.Fatalf("enabling SetCastShadow did not darken the frame: off=%d on=%d", notCasting, casting)
	}
}

// TestPipelineFollowsMaterialSwap is why the renderer re-resolves pipelines from the
// cached materials every frame rather than only on a structural change: swapping in a
// material of another type must move the mesh onto that type's pipeline and actually
// change how it renders.
func TestPipelineFollowsMaterialSwap(t *testing.T) {
	r, err := pix.NewOffscreenRenderer(64, 64)
	if err != nil {
		t.Fatal(err)
	}
	defer r.Destroy()
	scene := scenes.New()
	defer scene.Destroy()
	scene.SetAmbient(colors.RGB32F{0.8, 0.8, 0.8})

	red := r.NewBasicMaterial()
	red.SetColor(colors.RGBA32F{1, 0, 0, 1})
	blue := r.NewBlinnPhongMaterial()
	blue.SetColor(colors.RGBA32F{0, 0, 1, 1})

	mesh := scene.NewMesh(r.GeometryStore.Create(pix.BoxGeometry(1, 1, 1)), red)
	scene.Add(mesh)

	cam := cameras.NewPerspectiveCamera(45, 1, 0.1, 100)
	cam.SetPosition(glm.Vec3f{0, 0, 3})
	r.Render(scene, cam)
	px := r.Pixels()
	i := (32*64 + 32) * 4
	if px[i] < 200 || px[i+2] > 40 {
		t.Fatalf("before swap: center pixel = (%d,%d,%d), want red", px[i], px[i+1], px[i+2])
	}

	mesh.SetMaterial(blue)
	r.Render(scene, cam)
	px = r.Pixels()
	if px[i+2] < 200 || px[i] > 40 {
		t.Fatalf("after swap to blue Blinn-Phong: center pixel = (%d,%d,%d), want blue", px[i], px[i+1], px[i+2])
	}
}

// TestMaterialSwapWithinOnePool: two materials of the same type share a pool and so
// resolve to the same pipeline. Nothing about the draw changes except the record slot
// the drawable points at — which is now read straight off the mesh entry rather than
// through a table the producer deduped.
//
// The cross-pool swap above would still pass if that slot were wrong, because there the
// pipeline changes too and that alone repaints the frame. Here the pipeline is
// identical, so the colour can only come from the slot.
func TestMaterialSwapWithinOnePool(t *testing.T) {
	r, err := pix.NewOffscreenRenderer(64, 64)
	if err != nil {
		t.Fatal(err)
	}
	defer r.Destroy()
	scene := scenes.New()
	defer scene.Destroy()
	scene.SetAmbient(colors.RGB32F{0.8, 0.8, 0.8})

	red := r.NewBasicMaterial()
	red.SetColor(colors.RGBA32F{1, 0, 0, 1})
	green := r.NewBasicMaterial()
	green.SetColor(colors.RGBA32F{0, 1, 0, 1})

	mesh := scene.NewMesh(r.GeometryStore.Create(pix.BoxGeometry(1, 1, 1)), red)
	scene.Add(mesh)

	cam := cameras.NewPerspectiveCamera(45, 1, 0.1, 100)
	cam.SetPosition(glm.Vec3f{0, 0, 3})
	i := (32*64 + 32) * 4

	r.Render(scene, cam)
	if px := r.Pixels(); px[i] < 200 || px[i+1] > 40 {
		t.Fatalf("before swap: center pixel = (%d,%d,%d), want red", px[i], px[i+1], px[i+2])
	}

	mesh.SetMaterial(green)
	r.Render(scene, cam)
	if px := r.Pixels(); px[i+1] < 200 || px[i] > 40 {
		t.Fatalf("after swap to a second Basic material: center pixel = (%d,%d,%d), want green", px[i], px[i+1], px[i+2])
	}
}
