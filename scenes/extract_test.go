package scenes_test

import (
	"testing"

	"github.com/bluescreen10/pix/glm"
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

// TestPreviousTransformsDirtyFollowsAMoveByOneFrame covers what motion vectors need: a
// consumer keeping last frame's matrices must refresh them on the frame after a move,
// even though nothing moved on that frame, or a node that stopped keeps appearing to
// move.
func TestPreviousTransformsDirtyFollowsAMoveByOneFrame(t *testing.T) {
	scene := scenes.New()
	node := scene.NewGroup()

	var packet scenes.FramePacket
	scene.Extract(&packet)
	if packet.PreviousTransformsDirty {
		t.Fatal("PreviousTransformsDirty = true on the first extraction, want false")
	}

	node.SetPosition(glm.Vec3f{1, 0, 0})
	scene.Extract(&packet)
	if !packet.TransformsDirty || !packet.PreviousTransformsDirty {
		t.Fatalf("frame after creation, then a move: TransformsDirty = %v, PreviousTransformsDirty = %v, want true, true", packet.TransformsDirty, packet.PreviousTransformsDirty)
	}

	scene.Extract(&packet)
	if packet.TransformsDirty || !packet.PreviousTransformsDirty {
		t.Fatalf("frame after the move: TransformsDirty = %v, PreviousTransformsDirty = %v, want false, true", packet.TransformsDirty, packet.PreviousTransformsDirty)
	}

	scene.Extract(&packet)
	if packet.TransformsDirty || packet.PreviousTransformsDirty {
		t.Fatalf("two frames after the move: TransformsDirty = %v, PreviousTransformsDirty = %v, want false, false", packet.TransformsDirty, packet.PreviousTransformsDirty)
	}
}
