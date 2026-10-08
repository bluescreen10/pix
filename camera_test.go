package pix_test

import (
	"testing"

	"github.com/bluescreen10/pix"
	"github.com/bluescreen10/pix/colors"
	"github.com/bluescreen10/pix/glm"
	"github.com/bluescreen10/pix/scenes"
)

// cameraRigScene is a green cube at the origin seen by a camera riding a rig 3 units
// down +Z. The camera is the rig's child, at the rig's origin.
func cameraRigScene(t *testing.T, r *pix.Renderer) (*scenes.Scene, scenes.Group, scenes.Camera) {
	t.Helper()
	scene := scenes.New()
	scene.SetAmbient(colors.RGB32F{1, 1, 1}, 1)
	geo := r.GeometryStore.Create(pix.BoxGeometry(1, 1, 1))
	mat := r.NewBasicMaterial()
	mat.SetColor(colors.RGBA32F{0, 1, 0, 1})
	scene.NewMesh(geo, mat)
	geo.Release()

	rig := scene.NewGroup()
	rig.SetPosition(glm.Vec3f{0, 0, 3})
	camera := scene.NewPerspectiveCamera(45, 1, 0.1, 100)
	rig.Add(camera)
	return scene, rig, camera
}

func TestCameraRendersFromItsParent(t *testing.T) {
	r, err := pix.NewOffscreenRenderer(64, 64)
	if err != nil {
		t.Fatal(err)
	}
	defer r.Destroy()
	scene, rig, _ := cameraRigScene(t, r)
	defer scene.Destroy()

	r.Render(scene)
	if got := greenPixels(r.Pixels()); got == 0 {
		t.Fatal("the camera on the rig does not see the cube in front of it")
	}

	// Moving only the rig must carry the camera away from the cube.
	rig.SetPosition(glm.Vec3f{20, 0, 3})
	r.Render(scene)
	if got := greenPixels(r.Pixels()); got != 0 {
		t.Errorf("after moving the rig away, %d green pixels, want 0: the camera did not move with its parent", got)
	}
}

func TestHiddenCameraRendersNoScene(t *testing.T) {
	r, err := pix.NewOffscreenRenderer(64, 64)
	if err != nil {
		t.Fatal(err)
	}
	defer r.Destroy()
	scene, _, camera := cameraRigScene(t, r)
	defer scene.Destroy()

	camera.SetVisible(false)
	r.Render(scene)
	if got := greenPixels(r.Pixels()); got != 0 {
		t.Errorf("with the only camera hidden, %d green pixels, want 0", got)
	}

	camera.SetVisible(true)
	r.Render(scene)
	if got := greenPixels(r.Pixels()); got == 0 {
		t.Error("after SetVisible(true) the camera draws nothing")
	}
}
