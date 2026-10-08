package gltf_test

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/bluescreen10/pix"
	"github.com/bluescreen10/pix/glm"
	"github.com/bluescreen10/pix/loaders/gltf"
	"github.com/bluescreen10/pix/scenes"
	"github.com/chewxy/math32"
)

// cameraAsset is a glTF document with three camera nodes and nothing else:
//
//   - "front" holds the perspective camera "Main", 2 up and 5 back.
//   - "top" holds an unnamed orthographic camera, turned to look straight down.
//   - "side" holds a perspective camera with neither an aspect ratio nor a far plane.
func cameraAsset(t *testing.T) string {
	t.Helper()
	down := glm.NewQuat(-math32.Pi/2, glm.Vec3f{1, 0, 0})
	doc := map[string]any{
		"asset":  map[string]any{"version": "2.0"},
		"scene":  0,
		"scenes": []map[string]any{{"nodes": []int{0, 1, 2}}},
		"nodes": []map[string]any{
			{"name": "front", "camera": 0, "translation": []float32{0, 2, 5}},
			{"name": "top", "camera": 1, "translation": []float32{0, 10, 0}, "rotation": []float32{down[0], down[1], down[2], down[3]}},
			{"name": "side", "camera": 2, "translation": []float32{8, 0, 0}},
		},
		"cameras": []map[string]any{
			{"name": "Main", "type": "perspective", "perspective": map[string]any{"aspectRatio": 1.5, "yfov": 0.8, "znear": 0.1, "zfar": 50}},
			{"type": "orthographic", "orthographic": map[string]any{"xmag": 4, "ymag": 3, "znear": 0.5, "zfar": 20}},
			{"type": "perspective", "perspective": map[string]any{"yfov": 1, "znear": 0.2}},
		},
	}
	data, err := json.Marshal(doc)
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(t.TempDir(), "cameras.gltf")
	if err := os.WriteFile(path, data, 0o644); err != nil {
		t.Fatal(err)
	}
	return path
}

func loadCameraAsset(t *testing.T) (*pix.Renderer, *scenes.Scene) {
	t.Helper()
	r, err := pix.NewOffscreenRenderer(32, 16)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(r.Destroy)
	scene := scenes.New()
	t.Cleanup(scene.Destroy)
	if _, err := gltf.Load(r, scene, cameraAsset(t), nil); err != nil {
		t.Fatal(err)
	}
	scene.Sync()
	return r, scene
}

// expectMatrix fails unless got matches want to within float rounding. The tolerance
// is relative: a projection's depth terms are tiny when its far plane is distant, and an
// absolute one would let a far plane a hundred times off pass.
func expectMatrix(t *testing.T, what string, got, want glm.Mat4f) {
	t.Helper()
	for i := range got {
		if math32.Abs(got[i]-want[i]) > 1e-4*math32.Abs(want[i])+1e-7 {
			t.Errorf("%s = %v, want %v", what, got, want)
			return
		}
	}
}

func TestLoadCameras(t *testing.T) {
	r, scene := loadCameraAsset(t)
	cameras := scene.Cameras()
	if len(cameras) != 3 {
		t.Fatalf("the scene has %d cameras after loading, want 3", len(cameras))
	}

	t.Run("names", func(t *testing.T) {
		want := []string{"Main", "top", "side"}
		for i, cam := range cameras {
			if cam.Name() != want[i] {
				t.Errorf("Cameras()[%d].Name() = %q, want %q (the camera's name, else its node's)", i, cam.Name(), want[i])
			}
		}
	})

	t.Run("perspective", func(t *testing.T) {
		view := glm.Transform(glm.Vec3f{1, 1, 1}, glm.QuatfIdentity, glm.Vec3f{0, 2, 5}).Inv()
		want := glm.PerspectiveRevZRH(float32(0.8), 1.5, 0.1, 50).Mul4x4(view)
		expectMatrix(t, "Main ViewProjection()", cameras[0].ViewProjection(), want)
	})

	t.Run("orthographic at a turned node", func(t *testing.T) {
		down := glm.NewQuat(-math32.Pi/2, glm.Vec3f{1, 0, 0})
		view := glm.Transform(glm.Vec3f{1, 1, 1}, down, glm.Vec3f{0, 10, 0}).Inv()
		want := glm.OrthoFullRevZRH(float32(-4), 4, -3, 3, 0.5, 20).Mul4x4(view)
		expectMatrix(t, "top ViewProjection()", cameras[1].ViewProjection(), want)
	})

	t.Run("omitted aspect and far", func(t *testing.T) {
		view := glm.Transform(glm.Vec3f{1, 1, 1}, glm.QuatfIdentity, glm.Vec3f{8, 0, 0}).Inv()
		want := glm.PerspectiveRevZRH(float32(1), r.Aspect(), 0.2, 0.2*1e5).Mul4x4(view)
		expectMatrix(t, "side ViewProjection()", cameras[2].ViewProjection(), want)
	})
}

func TestLoadedCamerasStartHidden(t *testing.T) {
	_, scene := loadCameraAsset(t)

	var packet scenes.FramePacket
	scene.Extract(&packet)
	if len(packet.Views) != 0 {
		t.Fatalf("frame has %d views after loading, want 0: loaded cameras must not render unasked", len(packet.Views))
	}

	main, ok := scene.CameraByName("Main")
	if !ok {
		t.Fatal(`CameraByName("Main") found nothing after loading`)
	}
	main.SetVisible(true)
	scene.Extract(&packet)
	if len(packet.Views) != 1 {
		t.Errorf("frame has %d views after showing one loaded camera, want 1", len(packet.Views))
	}
}
