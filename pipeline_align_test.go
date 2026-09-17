package pix

import "testing"

// TestPipelineAlignmentDetachedMesh pins the parallel-array contract syncDrawList
// relies on: pipeBuf[i] must be the pipeline of the material drawables[i] references.
//
// It used to be resolved by a second walk of the payload lists that did not filter on
// flagAttached the way collectDrawables does, so a mesh that exists but is not in the
// graph (NewMesh without Add, or a later detach) shifted every following pipeline id
// by one. That is not just a wrong shader: gpuDrawable.materialID is an index into the
// *pool* of the material the pipeline was built for, so a shifted pair makes the draw
// read another material type's record buffer at that index.
func TestPipelineAlignmentDetachedMesh(t *testing.T) {
	r, err := NewOffscreenRenderer(64, 64)
	if err != nil {
		t.Fatal(err)
	}
	defer r.Destroy()
	scene := r.NewScene()
	defer scene.Destroy()

	geo := r.GeometryStore.Create(BoxGeometry(1, 1, 1))
	basic := r.NewBasicMaterial()
	phong := r.NewBlinnPhongMaterial()

	// Created but never added to the scene root: no drawable, but still in scene.meshes.
	scene.NewMesh(geo, basic)

	added := scene.NewMesh(geo, phong)
	scene.Add(added)

	r.syncDrawList(scene)
	pipes := scene.drawList.pipeBuf
	drawables, mats := scene.drawables, scene.drawMaterials

	if len(pipes) != len(drawables) {
		t.Fatalf("pipeBuf has %d entries, collectDrawables emitted %d drawables", len(pipes), len(drawables))
	}
	for i := range drawables {
		if want := r.pipelineForMaterial(mats[i]); pipes[i] != want {
			t.Errorf("drawable %d: pipeline %d, want %d (its own material's)", i, pipes[i], want)
		}
	}
	// The surviving drawable is the Blinn-Phong one; the orphaned basic material
	// must not have claimed its slot.
	if want := r.pipelineForMaterial(phong); len(pipes) > 0 && pipes[0] != want {
		t.Errorf("attached mesh got pipeline %d, want %d", pipes[0], want)
	}
}

// TestDrawableFlagsFollowShadowToggle covers Node.SetCastShadow/SetReceiveShadow: both
// bits land in gpuDrawable.flags, so toggling one after the draw list was built has to
// mark the drawables dirty or the change never reaches the GPU.
func TestDrawableFlagsFollowShadowToggle(t *testing.T) {
	r, err := NewOffscreenRenderer(64, 64)
	if err != nil {
		t.Fatal(err)
	}
	defer r.Destroy()
	scene := r.NewScene()
	defer scene.Destroy()

	mesh := scene.NewMesh(r.GeometryStore.Create(BoxGeometry(1, 1, 1)), r.NewBasicMaterial())
	scene.Add(mesh)
	mesh.SetCastShadow(false)
	r.syncDrawList(scene)
	if len(scene.drawables) != 1 {
		t.Fatalf("got %d drawables, want 1", len(scene.drawables))
	}
	if scene.drawables[0].flags&DrawableCastsShadow != 0 {
		t.Fatal("drawable casts shadow with the flag off")
	}

	mesh.SetCastShadow(true)
	r.syncDrawList(scene)
	if scene.drawables[0].flags&DrawableCastsShadow == 0 {
		t.Error("SetCastShadow(true) did not reach the drawable")
	}
}

// TestPipelineFollowsMaterialSwap is why syncDrawList re-resolves pipelines from the
// cached materials every frame rather than only on drawableDirty: swapping in a
// material of another type must move the mesh onto that type's pipeline.
func TestPipelineFollowsMaterialSwap(t *testing.T) {
	r, err := NewOffscreenRenderer(64, 64)
	if err != nil {
		t.Fatal(err)
	}
	defer r.Destroy()
	scene := r.NewScene()
	defer scene.Destroy()

	phong := r.NewBlinnPhongMaterial()
	mesh := scene.NewMesh(r.GeometryStore.Create(BoxGeometry(1, 1, 1)), r.NewBasicMaterial())
	scene.Add(mesh)
	r.syncDrawList(scene)

	mesh.SetMaterial(phong)
	r.syncDrawList(scene)
	if want := r.pipelineForMaterial(phong); scene.drawList.pipeBuf[0] != want {
		t.Errorf("after SetMaterial: pipeline %d, want %d", scene.drawList.pipeBuf[0], want)
	}
	if scene.drawables[0].materialID != phong.ID().Slot {
		t.Errorf("drawable materialID = %d, want %d", scene.drawables[0].materialID, phong.ID().Slot)
	}
}
