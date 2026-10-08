package scenes_test

import (
	"testing"

	"github.com/bluescreen10/pix/glm"
	"github.com/bluescreen10/pix/scenes"
	"github.com/chewxy/math32"
)

func TestExtractPublishesAttachedVisibleCameras(t *testing.T) {
	scene := scenes.New()
	scene.NewPerspectiveCamera(45, 1, 0.1, 100)
	hidden := scene.NewPerspectiveCamera(45, 1, 0.1, 100)
	detached := scene.NewPerspectiveCamera(45, 1, 0.1, 100)
	scene.NewOrthographicCamera(-1, 1, -1, 1, 0.1, 100)
	scene.Root().Remove(detached)
	hidden.SetVisible(false)

	var packet scenes.FramePacket
	scene.Extract(&packet)
	if len(packet.Views) != 2 {
		t.Fatalf("Extract published %d views, want 2 (the attached, visible cameras)", len(packet.Views))
	}
	if packet.Views[0].ID == packet.Views[1].ID {
		t.Errorf("two cameras share view ID %d", packet.Views[0].ID)
	}

	hidden.SetVisible(true)
	scene.Extract(&packet)
	if len(packet.Views) != 3 {
		t.Errorf("after SetVisible(true) Extract published %d views, want 3", len(packet.Views))
	}
}

func TestDestroyingACameraKeepsViewOrder(t *testing.T) {
	scene := scenes.New()
	var cameras []scenes.Camera
	for range 3 {
		camera := scene.NewPerspectiveCamera(45, 1, 0.1, 100)
		cameras = append(cameras, camera)
	}

	var packet scenes.FramePacket
	scene.Extract(&packet)
	second, third := packet.Views[1].ID, packet.Views[2].ID

	cameras[0].Destroy()
	scene.Extract(&packet)
	if len(packet.Views) != 2 {
		t.Fatalf("Extract published %d views after destroying one of 3, want 2", len(packet.Views))
	}
	if packet.Views[0].ID != second || packet.Views[1].ID != third {
		t.Errorf("view IDs = [%d %d], want [%d %d]", packet.Views[0].ID, packet.Views[1].ID, second, third)
	}
}

func TestCameraMovesWithItsParent(t *testing.T) {
	scene := scenes.New()
	vehicle := scene.NewGroup()
	camera := scene.NewPerspectiveCamera(45, 1, 0.1, 100)
	camera.SetPosition(glm.Vec3f{0, 2, 0})
	vehicle.Add(camera)

	vehicle.SetPosition(glm.Vec3f{10, 0, -5})
	var packet scenes.FramePacket
	scene.Extract(&packet)

	want := glm.Vec3f{10, 2, -5}
	if got := packet.Views[0].Position; !isNearlyEqual(got, want) {
		t.Errorf("view Position = %v, want %v", got, want)
	}
}

func TestCameraLookAtMatchesLookAtMatrix(t *testing.T) {
	eye := glm.Vec3f{4, 3, 5}
	target := glm.Vec3f{-1, 0.5, 2}

	scene := scenes.New()
	camera := scene.NewPerspectiveCamera(60, 1.5, 0.1, 100)
	camera.SetPosition(eye)
	camera.LookAt(target)

	var packet scenes.FramePacket
	scene.Extract(&packet)

	want := glm.LookAtRH(eye, target, glm.Vec3f{0, 1, 0})
	got := packet.Views[0].View
	for i := range got {
		if math32.Abs(got[i]-want[i]) > 1e-4 {
			t.Fatalf("view matrix = %v, want %v", got, want)
		}
	}

	wantForward := target.Sub(eye).Normalize()
	if got := camera.Forward(); !isNearlyEqual(got, wantForward) {
		t.Errorf("Forward() = %v, want %v", got, wantForward)
	}
}

func TestCameraSetUpKeepsForward(t *testing.T) {
	scene := scenes.New()
	camera := scene.NewPerspectiveCamera(60, 1, 0.1, 100)
	forward := glm.Vec3f{1, 0, 0}
	camera.SetForward(forward)
	camera.SetUp(glm.Vec3f{0, 0, 1})

	if got := camera.Forward(); !isNearlyEqual(got, forward) {
		t.Errorf("Forward() after SetUp = %v, want %v", got, forward)
	}
	if got, want := camera.Up(), (glm.Vec3f{0, 0, 1}); !isNearlyEqual(got, want) {
		t.Errorf("Up() = %v, want %v", got, want)
	}
}

func isNearlyEqual(a, b glm.Vec3f) bool {
	return a.Sub(b).Length() < 1e-4
}

func TestSceneCameras(t *testing.T) {
	scene := scenes.New()
	defer scene.Destroy()

	if got := scene.Cameras(); len(got) != 0 {
		t.Fatalf("Cameras() on an empty scene = %d cameras, want 0", len(got))
	}
	first := scene.NewPerspectiveCamera(45, 1, 0.1, 100)
	second := scene.NewOrthographicCamera(-1, 1, -1, 1, 0.1, 10)
	second.SetVisible(false)
	third := scene.NewPerspectiveCamera(60, 1, 0.1, 100)
	second.Destroy()

	got := scene.Cameras()
	want := []scenes.Camera{first, third}
	if len(got) != len(want) {
		t.Fatalf("Cameras() = %d cameras, want %d", len(got), len(want))
	}
	for i := range want {
		if got[i].ID() != want[i].ID() {
			t.Errorf("Cameras()[%d] = %v, want %v (creation order, destroyed ones gone)", i, got[i].ID(), want[i].ID())
		}
	}
}

func TestSceneCamerasIncludesHidden(t *testing.T) {
	scene := scenes.New()
	defer scene.Destroy()
	hidden := scene.NewPerspectiveCamera(45, 1, 0.1, 100)
	hidden.SetVisible(false)

	if got := scene.Cameras(); len(got) != 1 || got[0].ID() != hidden.ID() {
		t.Errorf("Cameras() = %v, want the hidden camera", got)
	}
}

func TestCameraByName(t *testing.T) {
	scene := scenes.New()
	defer scene.Destroy()

	// A group with the same name must not be mistaken for the camera.
	group := scene.NewGroup()
	group.SetName("overview")
	cam := scene.NewPerspectiveCamera(45, 1, 0.1, 100)
	cam.SetName("overview")

	got, ok := scene.CameraByName("overview")
	if !ok || got.ID() != cam.ID() {
		t.Errorf(`CameraByName("overview") = %v, %v, want the camera %v`, got.ID(), ok, cam.ID())
	}
	if _, ok := scene.CameraByName("closeup"); ok {
		t.Errorf(`CameraByName("closeup") found a camera, want none`)
	}
}
