package scenes_test

import (
	"testing"

	"github.com/bluescreen10/pix/glm"
	"github.com/bluescreen10/pix/scenes"
	"github.com/chewxy/math32"
)

func TestExtractPublishesAttachedVisibleCameras(t *testing.T) {
	scene := scenes.New()
	first := scene.NewPerspectiveCamera(45, 1, 0.1, 100)
	hidden := scene.NewPerspectiveCamera(45, 1, 0.1, 100)
	detached := scene.NewPerspectiveCamera(45, 1, 0.1, 100)
	last := scene.NewOrthographicCamera(-1, 1, -1, 1, 0.1, 100)
	scene.Add(first)
	scene.Add(hidden)
	scene.Add(last)
	hidden.SetVisible(false)
	_ = detached

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
		scene.Add(camera)
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
	scene.Add(vehicle)
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
	scene.Add(camera)
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
