package geometries_test

import (
	"testing"

	gputest "github.com/bluescreen10/gamekit/gpu/test"
	"github.com/bluescreen10/pix/geometries"
	"github.com/bluescreen10/pix/glm"
)

func newTestStore(t *testing.T) *geometries.Store {
	t.Helper()
	backend := gputest.New()
	if err := backend.Init(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(backend.Destroy)
	store := geometries.NewStore(backend)
	t.Cleanup(store.Destroy)
	return store
}

func triangleAttributes() []geometries.Attribute {
	positions := []glm.Vec3f{{-1, -1, 0}, {1, -1, 0}, {0, 1, 0}}
	return []geometries.Attribute{
		geometries.NewAttribute(geometries.AttributePosition, geometries.Float32x3, positions),
	}
}

func TestMorphTargets(t *testing.T) {
	store := newTestStore(t)
	smile := geometries.MorphTarget{
		Name:           "smile",
		PositionDeltas: []glm.Vec3f{{0, 0, 0}, {0, 0.5, 0}, {0, 0, 0}},
		NormalDeltas:   []glm.Vec3f{{0, 0, 0}, {0.1, 0, 0}, {0, 0, 0}},
	}
	blink := geometries.MorphTarget{
		Name:           "blink",
		PositionDeltas: []glm.Vec3f{{3, 4, 0}, {0, 0, 0}, {0, -1, 0}},
	}
	geo := store.Create(geometries.GeometryConfig{
		Attributes:   triangleAttributes(),
		MorphTargets: []geometries.MorphTarget{smile, blink},
	})
	defer geo.Release()

	if got := geo.MorphTargetCount(); got != 2 {
		t.Fatalf("MorphTargetCount() = %d, want 2", got)
	}

	t.Run("index by name", func(t *testing.T) {
		if index, ok := geo.MorphTargetIndex("blink"); !ok || index != 1 {
			t.Errorf(`MorphTargetIndex("blink") = %d, %v, want 1, true`, index, ok)
		}
		if _, ok := geo.MorphTargetIndex("frown"); ok {
			t.Errorf(`MorphTargetIndex("frown") found a target, want none`)
		}
	})

	t.Run("data round trip", func(t *testing.T) {
		got := geo.MorphTarget(0)
		if got.Name != "smile" {
			t.Errorf("MorphTarget(0).Name = %q, want %q", got.Name, "smile")
		}
		for i := range smile.PositionDeltas {
			if got.PositionDeltas[i] != smile.PositionDeltas[i] {
				t.Errorf("MorphTarget(0).PositionDeltas[%d] = %v, want %v", i, got.PositionDeltas[i], smile.PositionDeltas[i])
			}
			if got.NormalDeltas[i] != smile.NormalDeltas[i] {
				t.Errorf("MorphTarget(0).NormalDeltas[%d] = %v, want %v", i, got.NormalDeltas[i], smile.NormalDeltas[i])
			}
		}
		if got := geo.MorphTarget(1).NormalDeltas; got != nil {
			t.Errorf("MorphTarget(1).NormalDeltas = %v, want nil", got)
		}
	})

	t.Run("max displacement", func(t *testing.T) {
		if got := geo.MorphTargetMaxDisplacement(0); got != 0.5 {
			t.Errorf("MorphTargetMaxDisplacement(0) = %v, want 0.5", got)
		}
		if got := geo.MorphTargetMaxDisplacement(1); got != 5 {
			t.Errorf("MorphTargetMaxDisplacement(1) = %v, want 5", got)
		}
	})
}

func TestMorphTargetsAbsent(t *testing.T) {
	store := newTestStore(t)
	geo := store.Create(geometries.GeometryConfig{Attributes: triangleAttributes()})
	defer geo.Release()

	if got := geo.MorphTargetCount(); got != 0 {
		t.Errorf("MorphTargetCount() = %d, want 0", got)
	}
	if _, ok := geo.MorphTargetIndex("smile"); ok {
		t.Errorf(`MorphTargetIndex("smile") found a target on a geometry without targets`)
	}
}

func TestMorphTargetDeltaCountMismatchPanics(t *testing.T) {
	store := newTestStore(t)
	defer func() {
		if recover() == nil {
			t.Errorf("Create with 2 deltas for 3 vertices did not panic")
		}
	}()
	store.Create(geometries.GeometryConfig{
		Attributes: triangleAttributes(),
		MorphTargets: []geometries.MorphTarget{
			{Name: "short", PositionDeltas: []glm.Vec3f{{1, 0, 0}, {1, 0, 0}}},
		},
	})
}

func TestTangentAttribute(t *testing.T) {
	store := newTestStore(t)
	tangents := []glm.Vec4f{{1, 0, 0, 1}, {1, 0, 0, -1}, {0, 1, 0, 1}}
	geo := store.Create(geometries.GeometryConfig{
		Attributes: append(triangleAttributes(),
			geometries.NewAttribute(geometries.AttributeTangent, geometries.Float32x4, tangents)),
	})
	defer geo.Release()

	got := geo.AttributeData[glm.Vec4f](geometries.AttributeTangent)
	if len(got) != len(tangents) {
		t.Fatalf("tangent len = %d, want %d", len(got), len(tangents))
	}
	for i := range tangents {
		if got[i] != tangents[i] {
			t.Errorf("tangent[%d] = %v, want %v", i, got[i], tangents[i])
		}
	}
}

func TestCreateDeformOutput(t *testing.T) {
	store := newTestStore(t)

	t.Run("morphed source", func(t *testing.T) {
		geo := store.Create(geometries.GeometryConfig{
			Attributes: triangleAttributes(),
			MorphTargets: []geometries.MorphTarget{
				{Name: "lift", PositionDeltas: []glm.Vec3f{{0, 1, 0}, {0, 1, 0}, {0, 1, 0}}},
			},
		})
		defer geo.Release()

		output := geo.CreateDeformOutput()
		defer output.Release()
		if !output.IsValid() {
			t.Fatalf("CreateDeformOutput() returned an invalid geometry")
		}
		if got := output.MorphTargetCount(); got != 0 {
			t.Errorf("output MorphTargetCount() = %d, want 0", got)
		}
	})

	t.Run("plain source panics", func(t *testing.T) {
		geo := store.Create(geometries.GeometryConfig{Attributes: triangleAttributes()})
		defer geo.Release()
		defer func() {
			if recover() == nil {
				t.Errorf("CreateDeformOutput on a geometry without skin or morph targets did not panic")
			}
		}()
		geo.CreateDeformOutput()
	})
}

func TestLayoutRevisionChangesOnGrow(t *testing.T) {
	store := newTestStore(t)

	small := store.Create(geometries.GeometryConfig{Attributes: triangleAttributes()})
	defer small.Release()
	before := store.LayoutRevision()

	// 16384 positions are 192 KiB, more than a fresh position stream holds.
	positions := make([]glm.Vec3f, 16384)
	large := store.Create(geometries.GeometryConfig{
		Attributes: []geometries.Attribute{
			geometries.NewAttribute(geometries.AttributePosition, geometries.Float32x3, positions),
		},
	})
	defer large.Release()

	if store.LayoutRevision() == before {
		t.Errorf("LayoutRevision() = %d after a stream grow, want it changed from %d", store.LayoutRevision(), before)
	}
}

func TestLayoutRevisionStableWithoutGrow(t *testing.T) {
	store := newTestStore(t)
	before := store.LayoutRevision()

	geo := store.Create(geometries.GeometryConfig{Attributes: triangleAttributes()})
	defer geo.Release()

	if got := store.LayoutRevision(); got != before {
		t.Errorf("LayoutRevision() = %d after a create that fits, want %d", got, before)
	}
}
