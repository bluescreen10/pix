package scenes_test

import (
	"testing"

	"github.com/bluescreen10/pix/geometries"
	"github.com/bluescreen10/pix/glm"
	"github.com/bluescreen10/pix/scenes"
)

// publishedLOD extracts scene and returns the geometry drawn for mesh table entry
// mesh's level 1.
func publishedLOD(t *testing.T, scene *scenes.Scene, mesh int) geometries.ID {
	t.Helper()
	var packet scenes.FramePacket
	scene.Extract(&packet)
	entry := packet.Meshes.Data[mesh]
	if entry.LODRange.Count != 1 {
		t.Fatalf("mesh %d has %d coarser levels, want 1", mesh, entry.LODRange.Count)
	}
	return packet.LODs.Data[entry.LODRange.First].Geometry
}

func TestAddLODSharedLevelOnMorphedMesh(t *testing.T) {
	store := newGeometryStore(t)
	scene := scenes.New()
	defer scene.Destroy()
	base := morphedTriangle(t, store)
	level := base.CreateLOD([]uint32{0, 1, 2})
	defer level.Release()

	mesh := scene.NewMesh(base, newFakeMaterial())
	mesh.AddLOD(level, newFakeMaterial(), 10)

	var packet scenes.FramePacket
	scene.Extract(&packet)
	output := packet.Meshes.Data[0].Geometry
	drawn := publishedLOD(t, scene, 0)
	if drawn == level.ID() {
		t.Errorf("level 1 draws its own geometry %v, want its triangles over the morphed output", drawn)
	}
	if drawn == output {
		t.Errorf("level 1 draws the level-0 output %v itself, want its own triangles over it", drawn)
	}
}

func TestAddLODStaticLevelOnMorphedMesh(t *testing.T) {
	store := newGeometryStore(t)
	scene := scenes.New()
	defer scene.Destroy()
	impostor := plainTriangle(t, store)

	mesh := scene.NewMesh(morphedTriangle(t, store), newFakeMaterial())
	mesh.AddLOD(impostor, newFakeMaterial(), 10)

	if drawn := publishedLOD(t, scene, 0); drawn != impostor.ID() {
		t.Errorf("level 1 draws %v, want the impostor geometry %v as it is", drawn, impostor.ID())
	}
}

func TestAddLODMorphedLevelOfOtherVerticesPanics(t *testing.T) {
	store := newGeometryStore(t)
	scene := scenes.New()
	defer scene.Destroy()
	mesh := scene.NewMesh(morphedTriangle(t, store), newFakeMaterial())
	unrelated := morphedTriangle(t, store)

	defer func() {
		if recover() == nil {
			t.Errorf("AddLOD with another geometry's morph targets did not panic")
		}
	}()
	mesh.AddLOD(unrelated, newFakeMaterial(), 10)
}

func TestAddLODSharedLevelOnPlainMesh(t *testing.T) {
	store := newGeometryStore(t)
	scene := scenes.New()
	defer scene.Destroy()
	base := plainTriangle(t, store)
	level := base.CreateLOD([]uint32{0, 1, 2})
	defer level.Release()

	mesh := scene.NewMesh(base, newFakeMaterial())
	mesh.AddLOD(level, newFakeMaterial(), 10)

	if drawn := publishedLOD(t, scene, 0); drawn != level.ID() {
		t.Errorf("level 1 of an undeformed mesh draws %v, want the level geometry %v itself", drawn, level.ID())
	}
}

func TestSkinnedMeshAddLOD(t *testing.T) {
	store := newGeometryStore(t)
	scene := scenes.New()
	defer scene.Destroy()
	skeleton := scene.NewSkeleton(scenes.SkeletonConfig{
		Parents:     []int32{-1},
		InverseBind: []glm.Mat4f{glm.Mat4fIdentity},
		BindPose:    []scenes.Transform{{Rotation: glm.QuatfIdentity, Scale: glm.Vec3f{1, 1, 1}}},
	})
	base := skinnedTriangle(t, store)
	level := base.CreateLOD([]uint32{0, 1, 2})
	defer level.Release()

	mesh := scene.NewSkinnedMesh(base, newFakeMaterial(), skeleton)
	mesh.AddLOD(level, newFakeMaterial(), 10).SetLODHysteresis(2)

	var packet scenes.FramePacket
	scene.Extract(&packet)
	if got := packet.Meshes.Data[0].LODHysteresis; got != 2 {
		t.Errorf("LODHysteresis = %v, want 2", got)
	}
	if drawn := publishedLOD(t, scene, 0); drawn == level.ID() {
		t.Errorf("level 1 draws the unskinned level geometry %v, want its triangles over the skinned output", drawn)
	}
}

// TestLODLevelsSayWhetherTheyDrawDeformedVertices: a level made from a morphed mesh's
// geometry draws its morphed vertices, and a level of its own vertices does not. A
// renderer keeping deformed vertices' previous positions needs to tell them apart.
func TestLODLevelsSayWhetherTheyDrawDeformedVertices(t *testing.T) {
	store := newGeometryStore(t)
	scene := scenes.New()
	defer scene.Destroy()
	base := morphedTriangle(t, store)
	shared := base.CreateLOD([]uint32{0, 1, 2})
	defer shared.Release()
	static := plainTriangle(t, store)

	mesh := scene.NewMesh(base, newFakeMaterial())
	mesh.AddLOD(shared, newFakeMaterial(), 10)
	mesh.AddLOD(static, newFakeMaterial(), 20)

	var packet scenes.FramePacket
	scene.Extract(&packet)
	levels := packet.LODs.Data
	if len(levels) != 2 {
		t.Fatalf("%d coarser levels published, want 2", len(levels))
	}
	if !levels[0].DrawsDeformedVertices {
		t.Error("level made with CreateLOD: DrawsDeformedVertices = false, want true")
	}
	if levels[1].DrawsDeformedVertices {
		t.Error("level of its own vertices: DrawsDeformedVertices = true, want false")
	}
}
