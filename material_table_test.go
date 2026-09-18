package pix

import (
	"testing"

	"github.com/bluescreen10/pix/colors"
	"github.com/bluescreen10/pix/glm"
	"github.com/bluescreen10/pix/materials"
	"github.com/bluescreen10/pix/scenes"
)

// The drawables a scene emits reference materials through a distinct set
// (Scene.drawMaterials) rather than one entry each, which is what makes the renderer's
// per-frame pipeline work proportional to the materials in play instead of the objects
// on screen. These tests pin that: the set is deduped, the indirection still resolves
// to each drawable's own material, and a frame that changes nothing re-resolves the
// small set without touching the batch layout.

// TestMaterialTableDedupsAcrossInstances is the case the indirection exists for: an
// InstancedMesh emits one drawable per instance, and every one of them shares a single
// material.
func TestMaterialTableDedupsAcrossInstances(t *testing.T) {
	r, err := NewOffscreenRenderer(64, 64)
	if err != nil {
		t.Fatal(err)
	}
	defer r.Destroy()
	scene := scenes.New()
	defer scene.Destroy()

	geo := r.GeometryStore.Create(BoxGeometry(1, 1, 1))
	defer geo.Release()

	const instances = 64
	xforms := make([]glm.Mat4f, instances)
	for i := range xforms {
		xforms[i] = glm.Transform(glm.Vec3f{1, 1, 1}, glm.QuatIdentityf, glm.Vec3f{float32(i) * 2, 0, 0})
	}
	im := scene.NewInstancedMesh(geo, r.NewBasicMaterial(), xforms)
	scene.Add(im)

	r.prepareFrom(scene)

	if len(r.stateFor(scene.ID()).dl.drawables) != instances {
		t.Fatalf("got %d drawables, want %d (one per instance)", len(r.stateFor(scene.ID()).dl.drawables), instances)
	}
	if len(r.frame.Materials.Data) != 1 {
		t.Errorf("drawMaterials has %d entries, want 1: %d instances sharing one material "+
			"must not produce %d material entries", len(r.frame.Materials.Data), instances, instances)
	}
	for i, slot := range r.stateFor(scene.ID()).dl.drawMatSlot {
		if slot != 0 {
			t.Fatalf("drawable %d points at material slot %d, want 0", i, slot)
		}
	}
}

// TestMaterialTableKeepsDistinctMaterialsApart is the other half: dedup must key on
// material identity, so two materials of the SAME type stay separate entries.
func TestMaterialTableKeepsDistinctMaterialsApart(t *testing.T) {
	r, err := NewOffscreenRenderer(64, 64)
	if err != nil {
		t.Fatal(err)
	}
	defer r.Destroy()
	scene := scenes.New()
	defer scene.Destroy()

	geo := r.GeometryStore.Create(BoxGeometry(1, 1, 1))
	defer geo.Release()

	red, blue := r.NewBasicMaterial(), r.NewBasicMaterial()
	red.SetColor(colors.RGBA32F{1, 0, 0, 1})
	blue.SetColor(colors.RGBA32F{0, 0, 1, 1})

	// Two meshes on red, one on blue: three drawables, two distinct materials.
	for _, m := range []materials.Material{red, red, blue} {
		mesh := scene.NewMesh(geo, m)
		scene.Add(mesh)
	}
	r.prepareFrom(scene)

	if len(r.stateFor(scene.ID()).dl.drawables) != 3 {
		t.Fatalf("got %d drawables, want 3", len(r.stateFor(scene.ID()).dl.drawables))
	}
	if len(r.frame.Materials.Data) != 2 {
		t.Fatalf("drawMaterials has %d entries, want 2 (red and blue share a type, not an identity)",
			len(r.frame.Materials.Data))
	}
	// Every drawable must still resolve to the material it was built with: the entry the
	// indirection lands on has to be the one whose record slot the GPU drawable carries.
	for i := range r.stateFor(scene.ID()).dl.drawables {
		got := r.frame.Materials.Data[r.stateFor(scene.ID()).dl.drawMatSlot[i]]
		if got.Slot != r.stateFor(scene.ID()).dl.drawables[i].materialID {
			t.Errorf("drawable %d carries material slot %d but the table resolves to slot %d",
				i, r.stateFor(scene.ID()).dl.drawables[i].materialID, got.Slot)
		}
	}
}

// TestStaticSceneDoesNotRebuildBatches pins the steady state the whole design is for:
// re-syncing an unchanged scene re-resolves one pipeline per distinct material and
// stops there. A tint edit is likewise a record upload, not a batch rebuild — only a
// change to a material's PIPELINE (its blend mode here) may re-batch.
func TestStaticSceneDoesNotRebuildBatches(t *testing.T) {
	r, err := NewOffscreenRenderer(64, 64)
	if err != nil {
		t.Fatal(err)
	}
	defer r.Destroy()
	scene := scenes.New()
	defer scene.Destroy()

	geo := r.GeometryStore.Create(BoxGeometry(1, 1, 1))
	defer geo.Release()

	mat := r.NewBasicMaterial()
	for range 32 {
		scene.Add(scene.NewMesh(geo, mat))
	}

	r.prepareFrom(scene)
	after := r.stateFor(scene.ID()).dl.rebuilds
	if after == 0 {
		t.Fatal("first sync did not build the batch layout")
	}

	for range 10 {
		r.prepareFrom(scene)
	}
	if r.stateFor(scene.ID()).dl.rebuilds != after {
		t.Errorf("static scene rebuilt batches %d extra times over 10 frames",
			r.stateFor(scene.ID()).dl.rebuilds-after)
	}

	mat.SetColor(colors.RGBA32F{0, 1, 0, 1})
	r.prepareFrom(scene)
	if r.stateFor(scene.ID()).dl.rebuilds != after {
		t.Error("a tint edit rebuilt the batch layout; it changes a record, not a pipeline")
	}

	mat.SetBlend(materials.BlendAlpha)
	r.prepareFrom(scene)
	if r.stateFor(scene.ID()).dl.rebuilds == after {
		t.Error("a blend-mode change did NOT rebuild the batch layout; it changes the pipeline")
	}
}
