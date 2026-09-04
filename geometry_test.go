package pix

import (
	"testing"

	"github.com/bluescreen10/pix/geometries"
	"github.com/bluescreen10/pix/glm"
)

// TestAttributeRoundTrip verifies GetAttributeData[T] returns what NewGeometry stored
// and SetAttributeData[T] replaces it in place.
func TestAttributeRoundTrip(t *testing.T) {
	r, err := NewOffscreenRenderer(64, 64)
	if err != nil {
		t.Fatal(err)
	}
	defer r.Destroy()

	positions := []glm.Vec3f{{-1, -1, 0}, {1, -1, 0}, {0, 1, 0}}
	uvs := []glm.Vec2f{{0, 0}, {1, 0}, {0.5, 1}}
	geo := r.GeometryStore.Create(geometries.GeometryConfig{
		Attributes: []geometries.Attribute{
			geometries.NewAttribute(geometries.AttributePosition, geometries.Float32x3, positions),
			geometries.NewAttribute(geometries.AttributeUV, geometries.Float32x2, uvs),
		},
		Indices: []uint32{0, 1, 2},
	})
	defer geo.Release()

	got := geo.GetAttributeData[glm.Vec3f](geometries.AttributePosition)
	if len(got) != len(positions) {
		t.Fatalf("position len = %d, want %d", len(got), len(positions))
	}
	for i := range positions {
		if got[i] != positions[i] {
			t.Fatalf("position[%d] = %v, want %v", i, got[i], positions[i])
		}
	}

	// Absent attribute → nil.
	if n := geo.GetAttributeData[glm.Vec4f](geometries.AttributeColor); n != nil {
		t.Fatalf("absent color attribute = %v, want nil", n)
	}

	// Replace positions in place and read them back.
	moved := []glm.Vec3f{{-2, -2, 1}, {2, -2, 1}, {0, 2, 1}}
	geo.SetAttributeData(geometries.AttributePosition, moved)
	back := geo.GetAttributeData[glm.Vec3f](geometries.AttributePosition)
	for i := range moved {
		if back[i] != moved[i] {
			t.Fatalf("after set: position[%d] = %v, want %v", i, back[i], moved[i])
		}
	}
}
