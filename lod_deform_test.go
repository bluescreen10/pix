package pix_test

import (
	"testing"

	"github.com/bluescreen10/pix"
	"github.com/bluescreen10/pix/geometries"
	"github.com/bluescreen10/pix/glm"
	"github.com/bluescreen10/pix/materials"
	"github.com/bluescreen10/pix/scenes"
)

// lowerTriangle is half of morphSquare: the triangle under its diagonal.
var lowerTriangle = []uint32{0, 1, 2, 0, 2, 1}

// The morph tests' camera sits 8 units away, so a level starting at nearLevelDistance
// is the one drawn.
const nearLevelDistance = 1

// renderMorphedSquare draws morphSquare fully blended toward shiftRight, with coarser
// levels added by addLevels, and returns the frame.
func renderMorphedSquare(t *testing.T, r *pix.Renderer, addLevels func(mesh scenes.Mesh, geo geometries.Geometry)) []byte {
	t.Helper()
	scene := scenes.New()
	defer scene.Destroy()
	geo := r.GeometryStore.Create(morphSquare(shiftRight()))
	defer geo.Release()
	mesh := scene.NewMesh(geo, r.NewBasicMaterial())
	mesh.SetMorphTargetWeight(0, 1)
	addLevels(mesh, geo)
	addMorphTestCamera(scene)
	r.Render(scene)
	return append([]byte(nil), r.Capture()...)
}

// TestMorphedLODLevelIsMorphed draws a morphed square from the distance of its level 1,
// half of it made with CreateLOD. The half must be drawn where the morph moved the
// square — not where the square was modelled.
func TestMorphedLODLevelIsMorphed(t *testing.T) {
	r := newMorphTestRenderer(t)
	whole := renderMorphedSquare(t, r, func(scenes.Mesh, geometries.Geometry) {})
	half := renderMorphedSquare(t, r, func(mesh scenes.Mesh, geo geometries.Geometry) {
		level := geo.CreateLOD(lowerTriangle)
		defer level.Release()
		mesh.AddLOD(level, r.NewBasicMaterial(), nearLevelDistance)
	})

	expectSquareIn(t, half, "level 1", rightRegion, centerRegion)
	if wholeLit, halfLit := litPixelsIn(whole, rightRegion), litPixelsIn(half, rightRegion); halfLit >= wholeLit {
		t.Errorf("level 1 lit %d pixels, want fewer than level 0's %d: it has half the triangles", halfLit, wholeLit)
	}
}

// TestStaticLODLevelOnMorphedMesh gives a morphed square an impostor level of its own
// vertices, 2 units above. It is drawn as it is, and the morph leaves it alone.
func TestStaticLODLevelOnMorphedMesh(t *testing.T) {
	r := newMorphTestRenderer(t)
	frame := renderMorphedSquare(t, r, func(mesh scenes.Mesh, _ geometries.Geometry) {
		above := make([]glm.Vec3f, len(morphSquareCorners))
		for i, corner := range morphSquareCorners {
			above[i] = corner.Add(glm.Vec3f{0, 2, 0})
		}
		impostor := r.GeometryStore.Create(geometries.GeometryConfig{
			Attributes: []geometries.Attribute{
				geometries.NewAttribute(geometries.AttributePosition, geometries.Float32x3, above),
			},
			Indices: []uint32{0, 1, 2, 0, 2, 3, 0, 2, 1, 0, 3, 2},
		})
		defer impostor.Release()
		mesh.AddLOD(impostor, r.NewBasicMaterial(), nearLevelDistance)
	})
	expectSquareIn(t, frame, "impostor level", aboveRegion, rightRegion, centerRegion)
}

// TestMorphedLODLevelSurvivesGeometryStoreGrow holds a morphed square's weights while
// the store grows. Its level 1 draws the morphed output's vertices through copies of
// their bases; a grow moves the vertices, and the level must follow them.
//
// A grow repacks every geometry from the start of the new buffer, so vertices that
// already sat packed would land where they were and stale bases would still happen to
// be right. A filler geometry freed ahead of the square leaves a hole the repack
// closes, which is what makes the square's vertices actually move.
func TestMorphedLODLevelSurvivesGeometryStoreGrow(t *testing.T) {
	r := newMorphTestRenderer(t)
	scene := scenes.New()
	defer scene.Destroy()
	filler := r.GeometryStore.Create(geometries.GeometryConfig{
		Attributes: []geometries.Attribute{
			geometries.NewAttribute(geometries.AttributePosition, geometries.Float32x3, make([]glm.Vec3f, 1024)),
		},
	})
	geo := r.GeometryStore.Create(morphSquare(shiftRight()))
	defer geo.Release()
	level := geo.CreateLOD(lowerTriangle)
	defer level.Release()
	mesh := scene.NewMesh(geo, r.NewBasicMaterial())
	mesh.AddLOD(level, r.NewBasicMaterial(), nearLevelDistance)
	mesh.SetMorphTargetWeight(0, 1)
	addMorphTestCamera(scene)

	r.Render(scene)
	expectSquareIn(t, r.Capture(), "before the grow", rightRegion, centerRegion)

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
	expectSquareIn(t, r.Capture(), "after the grow", rightRegion, centerRegion)
}

// TestSkinnedLODLevelIsPosed moves a skinned square's only bone 2 units right and
// draws it from the distance of its level 1, made with CreateLOD: the level must be
// drawn posed, where the bone took the square.
func TestSkinnedLODLevelIsPosed(t *testing.T) {
	r := newMorphTestRenderer(t)
	scene := scenes.New()
	defer scene.Destroy()

	config := morphSquare()
	joints := []glm.Vec4[uint16]{{0, 0, 0, 0}, {0, 0, 0, 0}, {0, 0, 0, 0}, {0, 0, 0, 0}}
	weights := []glm.Vec4f{{1, 0, 0, 0}, {1, 0, 0, 0}, {1, 0, 0, 0}, {1, 0, 0, 0}}
	config.Attributes = append(config.Attributes,
		geometries.NewAttribute(geometries.AttributeSkinIndex, geometries.Uint16x4, joints),
		geometries.NewAttribute(geometries.AttributeSkinWeight, geometries.Float32x4, weights),
	)
	geo := r.GeometryStore.Create(config)
	defer geo.Release()
	level := geo.CreateLOD(lowerTriangle)
	defer level.Release()
	material := r.NewBasicMaterial()
	material.SetCull(materials.CullNone)

	skeleton := scene.NewSkeleton(scenes.SkeletonConfig{
		Parents:     []int32{-1},
		InverseBind: []glm.Mat4f{glm.Mat4fIdentity},
		BindPose:    []scenes.Transform{{Rotation: glm.QuatfIdentity, Scale: glm.Vec3f{1, 1, 1}}},
	})
	mesh := scene.NewSkinnedMesh(geo, material, skeleton)
	mesh.AddLOD(level, material, nearLevelDistance)
	addMorphTestCamera(scene)

	skeleton.Bone(0).SetPosition(glm.Vec3f{2, 0, 0})
	r.Render(scene)
	expectSquareIn(t, r.Capture(), "posed level 1", rightRegion, centerRegion)
}
