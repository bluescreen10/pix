package scenes_test

import (
	"testing"

	"github.com/bluescreen10/pix/glm"
	"github.com/bluescreen10/pix/scenes"
)

func TestNewNodesStartUnderRoot(t *testing.T) {
	store := newGeometryStore(t)
	scene := scenes.New()
	defer scene.Destroy()
	root := scene.Root().ID()

	skeleton := scene.NewSkeleton(scenes.SkeletonConfig{
		Parents:     []int32{-1},
		InverseBind: []glm.Mat4f{glm.Mat4fIdentity},
		BindPose:    []scenes.Transform{{Rotation: glm.QuatfIdentity, Scale: glm.Vec3f{1, 1, 1}}},
	})
	nodes := map[string]scenes.Node{
		"group":          scene.NewGroup().Node,
		"mesh":           scene.NewMesh(plainTriangle(t, store), newFakeMaterial()).Node,
		"instanced mesh": scene.NewInstancedMesh(plainTriangle(t, store), newFakeMaterial(), []glm.Mat4f{glm.Mat4fIdentity}).Node,
		"skeleton":       skeleton.Node,
		"skinned mesh":   scene.NewSkinnedMesh(skinnedTriangle(t, store), newFakeMaterial(), skeleton).Node,
		"camera":         scene.NewPerspectiveCamera(45, 1, 0.1, 100).Node,
	}
	for kind, node := range nodes {
		if got := node.Parent().ID(); got != root {
			t.Errorf("new %s's Parent() = %v, want the scene root %v", kind, got, root)
		}
	}
	if got := skeleton.Bone(0).Parent().ID(); got != skeleton.ID() {
		t.Errorf("bone 0's Parent() = %v, want its skeleton %v", got, skeleton.ID())
	}
}

func TestNewMeshIsDrawnWithoutAdd(t *testing.T) {
	store := newGeometryStore(t)
	scene := scenes.New()
	defer scene.Destroy()
	scene.NewMesh(plainTriangle(t, store), newFakeMaterial())

	var packet scenes.FramePacket
	scene.Extract(&packet)
	if len(packet.Meshes.Data) != 1 {
		t.Errorf("packet describes %d meshes after NewMesh, want 1", len(packet.Meshes.Data))
	}
}

func TestAddMovesNewNodeFromRoot(t *testing.T) {
	store := newGeometryStore(t)
	scene := scenes.New()
	defer scene.Destroy()
	group := scene.NewGroup()
	mesh := scene.NewMesh(plainTriangle(t, store), newFakeMaterial())

	group.Add(mesh)
	if got := mesh.Parent().ID(); got != group.ID() {
		t.Errorf("mesh's Parent() after group.Add = %v, want the group %v", got, group.ID())
	}
	for _, child := range scene.Root().Children() {
		if child.ID() == mesh.ID() {
			t.Errorf("the root still lists the mesh as a child after group.Add moved it")
		}
	}
}
