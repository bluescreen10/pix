package scenes_test

import (
	"testing"

	"github.com/bluescreen10/pix/glm"
	"github.com/bluescreen10/pix/scenes"
)

func TestMeshByName(t *testing.T) {
	store := newGeometryStore(t)
	scene := scenes.New()
	defer scene.Destroy()

	// A group with the same name must not be mistaken for the mesh.
	group := scene.NewGroup()
	group.SetName("face")
	mesh := scene.NewMesh(plainTriangle(t, store), newFakeMaterial())
	mesh.SetName("face")

	got, ok := scene.MeshByName("face")
	if !ok || got.ID() != mesh.ID() {
		t.Errorf(`MeshByName("face") = %v, %v, want the mesh %v`, got.ID(), ok, mesh.ID())
	}
	if _, ok := scene.MeshByName("hand"); ok {
		t.Errorf(`MeshByName("hand") found a mesh, want none`)
	}
}

func TestSkinnedMeshByName(t *testing.T) {
	store := newGeometryStore(t)
	scene := scenes.New()
	defer scene.Destroy()

	skeleton := scene.NewSkeleton(scenes.SkeletonConfig{
		Parents:     []int32{-1},
		InverseBind: []glm.Mat4f{glm.Mat4fIdentity},
		BindPose:    []scenes.Transform{{Rotation: glm.QuatfIdentity, Scale: glm.Vec3f{1, 1, 1}}},
	})
	plain := scene.NewMesh(plainTriangle(t, store), newFakeMaterial())
	plain.SetName("body")
	skinned := scene.NewSkinnedMesh(skinnedTriangle(t, store), newFakeMaterial(), skeleton)
	skinned.SetName("body")

	got, ok := scene.SkinnedMeshByName("body")
	if !ok || got.ID() != skinned.ID() {
		t.Errorf(`SkinnedMeshByName("body") = %v, %v, want the skinned mesh %v`, got.ID(), ok, skinned.ID())
	}
	if _, ok := scene.SkinnedMeshByName("tail"); ok {
		t.Errorf(`SkinnedMeshByName("tail") found a mesh, want none`)
	}
}

func TestInstancedMeshByName(t *testing.T) {
	store := newGeometryStore(t)
	scene := scenes.New()
	defer scene.Destroy()

	plain := scene.NewMesh(plainTriangle(t, store), newFakeMaterial())
	plain.SetName("rocks")
	field := scene.NewInstancedMesh(plainTriangle(t, store), newFakeMaterial(), []glm.Mat4f{glm.Mat4fIdentity})
	field.SetName("rocks")

	got, ok := scene.InstancedMeshByName("rocks")
	if !ok || got.ID() != field.ID() {
		t.Errorf(`InstancedMeshByName("rocks") = %v, %v, want the instanced mesh %v`, got.ID(), ok, field.ID())
	}
	if _, ok := scene.InstancedMeshByName("trees"); ok {
		t.Errorf(`InstancedMeshByName("trees") found a mesh, want none`)
	}
}
