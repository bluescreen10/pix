package scenes_test

import (
	"testing"

	"github.com/bluescreen10/pix/scenes"
)

func TestExtractFrameBelongsToScene(t *testing.T) {
	first := scenes.New()
	second := scenes.New()

	var packet scenes.FramePacket
	first.Extract(&packet)
	if packet.Frame != 1 {
		t.Fatalf("first scene's initial frame = %d, want 1", packet.Frame)
	}
	first.Extract(&packet)
	if packet.Frame != 2 {
		t.Fatalf("first scene's second frame = %d, want 2", packet.Frame)
	}

	second.Extract(&packet)
	if packet.Frame != 1 {
		t.Fatalf("second scene's initial frame = %d, want 1", packet.Frame)
	}
}

func TestTransformDirtinessSurvivesSyncUntilExtract(t *testing.T) {
	scene := scenes.New()
	scene.NewGroup()
	scene.Sync()
	scene.Sync()

	var packet scenes.FramePacket
	scene.Extract(&packet)
	if !packet.TransformsDirty {
		t.Fatal("TransformsDirty = false after repeated Sync calls, want true")
	}

	scene.Extract(&packet)
	if packet.TransformsDirty {
		t.Fatal("TransformsDirty = true without another transform change")
	}
}
