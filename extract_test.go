package pix

import (
	"testing"

	"github.com/bluescreen10/pix/colors"
	"github.com/bluescreen10/pix/glm"
	"github.com/bluescreen10/pix/scenes"
)

// TestExtractDescribesObjectsNotDrawRecords pins the shape of the boundary: the packet
// carries one entry per renderable OBJECT, and the renderer expands it into the flat
// per-instance, per-level draw records the GPU works on. The expected numbers below are
// written out rather than derived, so a change to either side has to disagree with a
// stated intent instead of with a second copy of the same loop.
func TestExtractDescribesObjectsNotDrawRecords(t *testing.T) {
	r, err := NewOffscreenRenderer(64, 64)
	if err != nil {
		t.Fatal(err)
	}
	defer r.Destroy()
	scene := scenes.New()
	defer scene.Destroy()

	near := r.GeometryStore.Create(BoxGeometry(1, 1, 1))
	far := r.GeometryStore.Create(BoxGeometry(2, 2, 2))
	defer near.Release()
	defer far.Release()

	red, blue := r.NewBasicMaterial(), r.NewBasicMaterial()
	red.SetColor(colors.RGBA32F{1, 0, 0, 1})
	blue.SetColor(colors.RGBA32F{0, 0, 1, 1})

	// A plain mesh; a mesh with one coarser level; a 3-instance mesh with one coarser
	// level. The last is what makes instance-major vs level-major ordering observable.
	scene.Add(scene.NewMesh(near, red))

	lodded := scene.NewMesh(near, red)
	lodded.AddLOD(far, blue, 10)
	scene.Add(lodded)

	xforms := []glm.Mat4f{
		glm.Transform(glm.Vec3f{1, 1, 1}, glm.QuatIdentityf, glm.Vec3f{-2, 0, 0}),
		glm.Transform(glm.Vec3f{1, 1, 1}, glm.QuatIdentityf, glm.Vec3f{2, 0, 0}),
		glm.Transform(glm.Vec3f{1, 1, 1}, glm.QuatIdentityf, glm.Vec3f{6, 0, 0}),
	}
	field := scene.NewInstancedMesh(near, blue, xforms)
	field.AddLOD(far, red, 20)
	scene.Add(field)

	// Created but never added: must appear in neither the packet nor the draw records.
	scene.NewMesh(near, red)

	scene.Sync()
	r.prepareFrom(scene)

	var p scenes.FramePacket
	scene.Extract(&p)

	// Three attached objects, however many times each is drawn.
	if len(p.Meshes.Data) != 3 {
		t.Fatalf("packet describes %d objects, want 3 (the unattached mesh must not appear)",
			len(p.Meshes.Data))
	}
	// Two coarser levels, one per LOD-tagged object. Level 0 is on the mesh, not here.
	if len(p.LODs.Data) != 2 {
		t.Errorf("packet has %d coarser LOD levels, want 2", len(p.LODs.Data))
	}
	// Red and blue, each referenced several times across meshes and levels.
	if len(p.Materials.Data) != 2 {
		t.Errorf("packet has %d distinct materials, want 2", len(p.Materials.Data))
	}
	if p.Source != scene.ID() {
		t.Errorf("packet source %d, want the scene's %d", p.Source, scene.ID())
	}

	// 1 plain + 2 levels + (3 instances x 2 levels) = 9 draw records.
	const wantRecords = 1 + 2 + 3*2
	got := r.stateFor(scene.ID()).dl.drawables
	if len(got) != wantRecords {
		t.Fatalf("expanded to %d draw records, want %d", len(got), wantRecords)
	}

	// Instances outer, levels inner: the instanced mesh's six records must read
	// (instance0 level0, instance0 level1, instance1 level0, ...).
	inst := got[3:]
	for i, rec := range inst {
		wantInstance, wantLevel := uint32(i/2), uint32(i%2)
		if rec.lodLevel != wantLevel {
			t.Errorf("instanced record %d: level %d, want %d (levels must vary fastest)",
				i, rec.lodLevel, wantLevel)
		}
		if rec.transformID != inst[0].transformID+wantInstance {
			t.Errorf("instanced record %d: transform %d, want %d",
				i, rec.transformID, inst[0].transformID+wantInstance)
		}
	}

	// Every LOD-tagged object gets one entry in the renderer's LOD config table, and
	// slot 0 stays reserved so a lodID of 0 means "not LOD-tagged".
	if len(r.stateFor(scene.ID()).dl.lodEntries) != 3 {
		t.Errorf("LOD config table has %d entries, want 3 (reserved slot + two groups)",
			len(r.stateFor(scene.ID()).dl.lodEntries))
	}
	if got[0].lodID != 0 {
		t.Errorf("the plain mesh's record has lodID %d, want 0", got[0].lodID)
	}
	if e := r.stateFor(scene.ID()).dl.lodEntries[got[1].lodID]; e.levelCount != 2 || e.boundaries[0] != 10 {
		t.Errorf("LOD group = {levelCount %d, boundary %v}, want {2, 10}", e.levelCount, e.boundaries[0])
	}
}

// TestExtractBorrowsTransforms pins the O(1) handoff: extraction must hand over the
// scene's transform storage, not a copy of it, however large the scene — and an
// unchanged scene must not be rewalked at all.
func TestExtractBorrowsTransforms(t *testing.T) {
	r, err := NewOffscreenRenderer(64, 64)
	if err != nil {
		t.Fatal(err)
	}
	defer r.Destroy()
	scene := scenes.New()
	defer scene.Destroy()

	geo := r.GeometryStore.Create(BoxGeometry(1, 1, 1))
	defer geo.Release()
	mesh := scene.NewMesh(geo, r.NewBasicMaterial())
	scene.Add(mesh)
	scene.Sync()

	var p scenes.FramePacket
	scene.Extract(&p)
	if len(p.Transforms.Data) == 0 {
		t.Fatal("packet carries no transforms")
	}

	// Borrowing, shown without reaching into the scene: move a node and settle it, and
	// the ALREADY-EXTRACTED packet must see the new matrix. A packet holding a copy
	// would still be showing the old one.
	slot := p.Meshes.Data[0].Transforms.First
	before := p.Transforms.Data[slot]
	mesh.SetPosition(glm.Vec3f{7, 0, 0})
	scene.Sync()
	if p.Transforms.Data[slot] == before {
		t.Error("Extract copied the transform table instead of borrowing it")
	}

	// An unchanged scene keeps the same mesh-table revision, which is what lets a
	// consumer skip the table entirely.
	rev := p.Meshes.Revision
	scene.Extract(&p)
	if p.Meshes.Revision != rev {
		t.Errorf("extracting an unchanged scene advanced the mesh revision %d -> %d", rev, p.Meshes.Revision)
	}
	// Adding an object must advance it.
	scene.Add(scene.NewMesh(geo, r.NewBasicMaterial()))
	scene.Extract(&p)
	if p.Meshes.Revision == rev {
		t.Error("adding a mesh did not advance the mesh revision")
	}

	allocs := testing.AllocsPerRun(20, func() {
		scene.Extract(&p)
	})
	if allocs != 0 {
		t.Errorf("steady-state Extract allocated %.1f times per call, want 0", allocs)
	}
}

// TestExtractIsReadOnly pins what replaced the receipt/commit protocol: extraction
// borrows, it never consumes. Extracting a scene any number of times must leave it
// exactly as it was, so a caller that extracts and then decides not to render loses
// nothing and double-counts nothing.
//
// Particles are the case that matters, because a simulation step has to happen exactly
// once. Rendering is what consumes the queued births — see Scene.Rendered,
// which Render calls after submitting.
func TestExtractIsReadOnly(t *testing.T) {
	_, scene, config := newParticleTestScene(t)
	config.Emitters = []scenes.ParticleEmitter{&countEmitter{n: 5}}
	c := scene.NewParticleContainer(config, 32)
	scene.Add(c)
	scene.Sync()

	c.Update(0.1) // queue some births and a dt
	births, dt, alive := len(c.Pending()), c.PendingStep(), c.Alive()
	if births == 0 {
		t.Fatal("emitter queued no births; the test cannot show anything")
	}

	var p scenes.FramePacket
	for range 3 {
		scene.Extract(&p)
		if len(c.Pending()) != births {
			t.Fatalf("Extract consumed the pending births: %d left, want %d", len(c.Pending()), births)
		}
		if c.PendingStep() != dt {
			t.Fatalf("Extract consumed the accumulated dt: %v, want %v", c.PendingStep(), dt)
		}
		if c.Alive() != alive {
			t.Fatalf("Extract advanced alive to %d without anything being rendered", c.Alive())
		}
		if got := int(p.Particles.Data[0].Newborns.Count); got != births {
			t.Fatalf("packet carries %d births, want %d", got, births)
		}
	}

	// Rendering is what consumes them — and only then.
	scene.Rendered()
	if len(c.Pending()) != 0 || c.PendingStep() != 0 {
		t.Errorf("after retire: %d pending, dt %v; want 0 and 0", len(c.Pending()), c.PendingStep())
	}
	if c.Alive() != alive+uint32(births) {
		t.Errorf("alive = %d, want %d", c.Alive(), alive+uint32(births))
	}
}
