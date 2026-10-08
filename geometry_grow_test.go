package pix_test

import (
	"testing"

	"github.com/bluescreen10/pix/geometries"
	"github.com/bluescreen10/pix/glm"
	"github.com/bluescreen10/pix/scenes"
)

// TestMeshSurvivesGeometryStoreGrow draws a plain mesh, grows the geometry store so
// that its data moves, and draws it again. The draw commands carry each batch's index
// range, copied out of the store; a grow that moves the ranges has to rebuild them, or
// the mesh is drawn from wherever its triangles used to be.
//
// A filler geometry freed ahead of the mesh leaves a hole the grow's repack closes —
// without it, the mesh's data would land where it was and stale ranges would still
// happen to be right.
func TestMeshSurvivesGeometryStoreGrow(t *testing.T) {
	r := newMorphTestRenderer(t)
	scene := scenes.New()
	defer scene.Destroy()
	filler := r.GeometryStore.Create(geometries.GeometryConfig{
		Attributes: []geometries.Attribute{
			geometries.NewAttribute(geometries.AttributePosition, geometries.Float32x3, make([]glm.Vec3f, 1024)),
		},
	})
	geo := r.GeometryStore.Create(morphSquare())
	defer geo.Release()
	scene.NewMesh(geo, r.NewBasicMaterial())
	addMorphTestCamera(scene)

	r.Render(scene)
	expectSquareIn(t, r.Capture(), "before the grow", centerRegion)

	filler.Release()
	layoutRevision := r.GeometryStore.LayoutRevision()
	large := r.GeometryStore.Create(geometries.GeometryConfig{
		Attributes: []geometries.Attribute{
			geometries.NewAttribute(geometries.AttributePosition, geometries.Float32x3, make([]glm.Vec3f, 1<<18)),
		},
	})
	defer large.Release()
	if r.GeometryStore.LayoutRevision() == layoutRevision {
		t.Fatal("creating 4 MiB of positions did not grow the geometry store; the test needs a grow")
	}

	r.Render(scene)
	expectSquareIn(t, r.Capture(), "after the grow", centerRegion)
}
