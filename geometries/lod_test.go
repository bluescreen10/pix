package geometries_test

import (
	"testing"

	"github.com/bluescreen10/pix/geometries"
	"github.com/bluescreen10/pix/glm"
)

// quadConfig is a 2x2 square of two triangles in the XY plane, with one morph target
// lifting every corner.
func quadConfig() geometries.GeometryConfig {
	positions := []glm.Vec3f{{-1, -1, 0}, {1, -1, 0}, {1, 1, 0}, {-1, 1, 0}}
	lift := glm.Vec3f{0, 1, 0}
	return geometries.GeometryConfig{
		Attributes: []geometries.Attribute{
			geometries.NewAttribute(geometries.AttributePosition, geometries.Float32x3, positions),
		},
		Indices: []uint32{0, 1, 2, 0, 2, 3},
		MorphTargets: []geometries.MorphTarget{
			{Name: "lift", PositionDeltas: []glm.Vec3f{lift, lift, lift, lift}},
		},
	}
}

func TestCreateLOD(t *testing.T) {
	store := newTestStore(t)
	base := store.Create(quadConfig())
	defer base.Release()
	lowerTriangle := []uint32{0, 1, 2}
	lod := base.CreateLOD(lowerTriangle)
	defer lod.Release()

	t.Run("own indices", func(t *testing.T) {
		got := lod.Indices()
		if len(got) != len(lowerTriangle) {
			t.Fatalf("Indices() = %v, want %v", got, lowerTriangle)
		}
		for i := range lowerTriangle {
			if got[i] != lowerTriangle[i] {
				t.Errorf("Indices()[%d] = %d, want %d", i, got[i], lowerTriangle[i])
			}
		}
	})

	t.Run("base's vertices", func(t *testing.T) {
		if got := lod.VertexCount(); got != 4 {
			t.Errorf("VertexCount() = %d, want the base's 4", got)
		}
		got := lod.AttributeData[glm.Vec3f](geometries.AttributePosition)
		want := base.AttributeData[glm.Vec3f](geometries.AttributePosition)
		if len(got) != len(want) {
			t.Fatalf("positions = %v, want the base's %v", got, want)
		}
		for i := range want {
			if got[i] != want[i] {
				t.Errorf("position[%d] = %v, want the base's %v", i, got[i], want[i])
			}
		}
		if got := lod.MorphTargetCount(); got != 1 {
			t.Errorf("MorphTargetCount() = %d, want the base's 1", got)
		}
	})

}

func TestLODBoundsCoverOnlyItsVertices(t *testing.T) {
	store := newTestStore(t)
	// A unit triangle near the origin and one vertex 100 units away.
	positions := []glm.Vec3f{{0, 0, 0}, {1, 0, 0}, {0, 1, 0}, {100, 0, 0}}
	base := store.Create(geometries.GeometryConfig{
		Attributes: []geometries.Attribute{
			geometries.NewAttribute(geometries.AttributePosition, geometries.Float32x3, positions),
		},
		Indices: []uint32{0, 1, 2, 1, 3, 2},
	})
	defer base.Release()
	lod := base.CreateLOD([]uint32{0, 1, 2})
	defer lod.Release()

	bounds := lod.BoundingSphere()
	if bounds.Radius > 2 {
		t.Errorf("BoundingSphere().Radius = %v, want the unit triangle's (under 2), not one reaching the unused vertex 100 away", bounds.Radius)
	}
	for _, i := range lod.Indices() {
		if d := positions[i].Sub(bounds.Center).Length(); d > bounds.Radius+1e-4 {
			t.Errorf("vertex %d at %v lies %v from the center, outside BoundingSphere() %v", i, positions[i], d, bounds)
		}
	}
}

func TestSharesVerticesWith(t *testing.T) {
	store := newTestStore(t)
	base := store.Create(quadConfig())
	defer base.Release()
	other := store.Create(quadConfig())
	defer other.Release()
	lod := base.CreateLOD([]uint32{0, 1, 2})
	defer lod.Release()
	lodOfLOD := lod.CreateLOD([]uint32{0, 2, 3})
	defer lodOfLOD.Release()

	tests := []struct {
		name string
		a, b geometries.Geometry
		want bool
	}{
		{"itself", base, base, true},
		{"level with its base", lod, base, true},
		{"base with its level", base, lod, true},
		{"level of a level with the base", lodOfLOD, base, true},
		{"two levels of one base", lod, lodOfLOD, true},
		{"unrelated geometry", lod, other, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.a.SharesVerticesWith(tt.b); got != tt.want {
				t.Errorf("SharesVerticesWith() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestLODKeepsVerticesAlive(t *testing.T) {
	store := newTestStore(t)
	base := store.Create(quadConfig())
	lod := base.CreateLOD([]uint32{0, 1, 2})

	base.Release()
	if !base.IsValid() {
		t.Fatal("base freed while a level of detail still draws its vertices")
	}
	if got := lod.VertexCount(); got != 4 {
		t.Errorf("level's VertexCount() = %d after releasing the base, want 4", got)
	}

	lod.Release()
	if base.IsValid() {
		t.Error("base still alive after its last handle and its only level were released")
	}
}

func TestCreateLODIndexPastVerticesPanics(t *testing.T) {
	store := newTestStore(t)
	base := store.Create(quadConfig())
	defer base.Release()
	defer func() {
		if recover() == nil {
			t.Errorf("CreateLOD with index 4 into 4 vertices did not panic")
		}
	}()
	base.CreateLOD([]uint32{0, 1, 4})
}

func TestIsSkinned(t *testing.T) {
	store := newTestStore(t)
	plain := store.Create(quadConfig())
	defer plain.Release()
	if plain.IsSkinned() {
		t.Error("IsSkinned() = true for a geometry without skin attributes")
	}

	config := quadConfig()
	joints := []glm.Vec4[uint16]{{0, 0, 0, 0}, {0, 0, 0, 0}, {0, 0, 0, 0}, {0, 0, 0, 0}}
	weights := []glm.Vec4f{{1, 0, 0, 0}, {1, 0, 0, 0}, {1, 0, 0, 0}, {1, 0, 0, 0}}
	config.Attributes = append(config.Attributes,
		geometries.NewAttribute(geometries.AttributeSkinIndex, geometries.Uint16x4, joints),
		geometries.NewAttribute(geometries.AttributeSkinWeight, geometries.Float32x4, weights))
	skinned := store.Create(config)
	defer skinned.Release()
	lod := skinned.CreateLOD([]uint32{0, 1, 2})
	defer lod.Release()
	if !skinned.IsSkinned() || !lod.IsSkinned() {
		t.Errorf("IsSkinned() = %v for the skinned geometry and %v for its level, want true for both", skinned.IsSkinned(), lod.IsSkinned())
	}
}
