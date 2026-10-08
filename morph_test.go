package pix_test

import (
	"testing"

	"github.com/bluescreen10/pix"
	"github.com/bluescreen10/pix/colors"
	"github.com/bluescreen10/pix/geometries"
	"github.com/bluescreen10/pix/glm"
	"github.com/bluescreen10/pix/materials"
	"github.com/bluescreen10/pix/scenes"
)

// morphViewSize is the square frame the morph tests render. Their camera sits 8 units
// from the origin with a 45° field of view, so a point 2 units off center lands about
// 19 pixels from the middle.
const morphViewSize = 64

// Regions of the frame the morph tests look for a small square in: at the origin, 2
// units right of it, and 2 units above it. Each is [x0, y0, x1, y1), row 0 at the top.
var (
	centerRegion = [4]int{28, 28, 36, 36}
	rightRegion  = [4]int{46, 28, 58, 36}
	aboveRegion  = [4]int{28, 6, 36, 18}
)

// morphSquareCorners is a half-unit square facing +Z, centered on the origin.
var morphSquareCorners = []glm.Vec3f{{-0.25, -0.25, 0}, {0.25, -0.25, 0}, {0.25, 0.25, 0}, {-0.25, 0.25, 0}}

// shiftRight is a morph target moving every corner of morphSquareCorners 2 units right.
func shiftRight() geometries.MorphTarget {
	right := glm.Vec3f{2, 0, 0}
	return geometries.MorphTarget{Name: "right", PositionDeltas: []glm.Vec3f{right, right, right, right}}
}

// morphSquare is morphSquareCorners with the given targets, drawn from both sides.
func morphSquare(targets ...geometries.MorphTarget) geometries.GeometryConfig {
	return geometries.GeometryConfig{
		Attributes: []geometries.Attribute{
			geometries.NewAttribute(geometries.AttributePosition, geometries.Float32x3, morphSquareCorners),
		},
		Indices:      []uint32{0, 1, 2, 0, 2, 3, 0, 2, 1, 0, 3, 2},
		MorphTargets: targets,
	}
}

func newMorphTestRenderer(t *testing.T) *pix.Renderer {
	t.Helper()
	r, err := pix.NewOffscreenRenderer(morphViewSize, morphViewSize)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(r.Destroy)
	r.SetClearColor(colors.RGBA32F{0, 0, 0, 1})
	return r
}

// addMorphTestCamera looks at the origin from 8 units along +Z.
func addMorphTestCamera(scene *scenes.Scene) {
	cam := scene.NewPerspectiveCamera(45, 1, 0.1, 100)
	cam.SetPosition(glm.Vec3f{0, 0, 8})
	cam.LookAt(glm.Vec3f{})
}

// litPixelsIn counts the pixels in region of an RGBA8 frame brighter than the clear
// color.
func litPixelsIn(pixels []byte, region [4]int) int {
	n := 0
	for y := region[1]; y < region[3]; y++ {
		for x := region[0]; x < region[2]; x++ {
			i := (y*morphViewSize + x) * 4
			if pixels[i] > 10 || pixels[i+1] > 10 || pixels[i+2] > 10 {
				n++
			}
		}
	}
	return n
}

// expectSquareIn fails the test unless the square is drawn in want and nowhere in
// others.
func expectSquareIn(t *testing.T, pixels []byte, step string, want [4]int, others ...[4]int) {
	t.Helper()
	if n := litPixelsIn(pixels, want); n == 0 {
		t.Errorf("%s: no lit pixels in region %v, want the square there", step, want)
	}
	for _, other := range others {
		if n := litPixelsIn(pixels, other); n != 0 {
			t.Errorf("%s: %d lit pixels in region %v, want none", step, n, other)
		}
	}
}

// TestMorphTargetMovesMesh renders a square at rest, fully blended toward a target that
// moves it right, and back at rest — the last step checks that returning every weight
// to 0 rewrites the output rather than leaving the morphed shape behind.
func TestMorphTargetMovesMesh(t *testing.T) {
	r := newMorphTestRenderer(t)
	scene := scenes.New()
	defer scene.Destroy()

	geo := r.GeometryStore.Create(morphSquare(shiftRight()))
	defer geo.Release()
	mat := r.NewBasicMaterial()
	mesh := scene.NewMesh(geo, mat)
	addMorphTestCamera(scene)

	r.Render(scene)
	expectSquareIn(t, r.Capture(), "at rest", centerRegion, rightRegion)

	mesh.SetMorphTargetWeight(0, 1)
	r.Render(scene)
	expectSquareIn(t, r.Capture(), "weight 1", rightRegion, centerRegion)

	r.Render(scene)
	expectSquareIn(t, r.Capture(), "weight 1, next frame", rightRegion, centerRegion)

	mesh.SetMorphTargetWeight(0, 0)
	r.Render(scene)
	expectSquareIn(t, r.Capture(), "back at rest", centerRegion, rightRegion)
}

// TestInstancedMeshMorphs checks that an instanced mesh's instances all draw the morphed
// shape.
func TestInstancedMeshMorphs(t *testing.T) {
	r := newMorphTestRenderer(t)
	scene := scenes.New()
	defer scene.Destroy()

	geo := r.GeometryStore.Create(morphSquare(shiftRight()))
	defer geo.Release()
	mat := r.NewBasicMaterial()
	// The second instance sits 2 units up: shifted right with the first, it lands in
	// neither the center nor the above region.
	transforms := []glm.Mat4f{
		glm.Mat4fIdentity,
		glm.Transform(glm.Vec3f{1, 1, 1}, glm.QuatfIdentity, glm.Vec3f{0, 2, 0}),
	}
	field := scene.NewInstancedMesh(geo, mat, transforms)
	addMorphTestCamera(scene)

	field.SetMorphTargetWeight(0, 1)
	r.Render(scene)
	expectSquareIn(t, r.Capture(), "weight 1", rightRegion, centerRegion, aboveRegion)
}

// TestMorphTargetsApplyBeforeSkinning blends a square 2 units right while its bone is
// turned 90° about Z. Morphed first, as glTF requires, the shift is turned with the
// square and it ends up 2 units above; skinned first, it would end up 2 units right.
func TestMorphTargetsApplyBeforeSkinning(t *testing.T) {
	r := newMorphTestRenderer(t)
	scene := scenes.New()
	defer scene.Destroy()

	config := morphSquare(shiftRight())
	joints := []glm.Vec4[uint16]{{0, 0, 0, 0}, {0, 0, 0, 0}, {0, 0, 0, 0}, {0, 0, 0, 0}}
	weights := []glm.Vec4f{{1, 0, 0, 0}, {1, 0, 0, 0}, {1, 0, 0, 0}, {1, 0, 0, 0}}
	config.Attributes = append(config.Attributes,
		geometries.NewAttribute(geometries.AttributeSkinIndex, geometries.Uint16x4, joints),
		geometries.NewAttribute(geometries.AttributeSkinWeight, geometries.Float32x4, weights),
	)
	geo := r.GeometryStore.Create(config)
	defer geo.Release()
	mat := r.NewBasicMaterial()
	mat.SetCull(materials.CullNone)

	skeleton := scene.NewSkeleton(scenes.SkeletonConfig{
		Parents:     []int32{-1},
		InverseBind: []glm.Mat4f{glm.Mat4fIdentity},
		BindPose:    []scenes.Transform{{Rotation: glm.QuatfIdentity, Scale: glm.Vec3f{1, 1, 1}}},
	})
	mesh := scene.NewSkinnedMesh(geo, mat, skeleton)
	addMorphTestCamera(scene)

	skeleton.Bone(0).RotateZ(glm.ToRadians(float32(90)))
	mesh.SetMorphTargetWeight(0, 1)
	r.Render(scene)
	expectSquareIn(t, r.Capture(), "turned and morphed", aboveRegion, rightRegion, centerRegion)
}

// TestMorphOutputSurvivesGeometryStoreGrow holds a morphed square's weights still while
// the geometry store grows. A grow discards every deform output's contents, so the
// renderer must rewrite the output even though the weights did not change; and it
// moves every geometry, so the output must follow the index range it borrows from its
// source. A filler geometry freed ahead of the square leaves a hole the grow's repack
// closes — without it, the square's data would land where it was and a stale range
// would still happen to be right.
func TestMorphOutputSurvivesGeometryStoreGrow(t *testing.T) {
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
	mat := r.NewBasicMaterial()
	mesh := scene.NewMesh(geo, mat)
	addMorphTestCamera(scene)

	mesh.SetMorphTargetWeight(0, 1)
	r.Render(scene)
	expectSquareIn(t, r.Capture(), "before the grow", rightRegion, centerRegion)

	filler.Release()
	layoutRevision := r.GeometryStore.LayoutRevision()
	// 4 MiB of positions, more than the position stream holds after the renderer's
	// own geometry.
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

// TestMorphTargetTurnsNormals lights a square facing the light, then fully blends it
// toward a target that turns its normal away: the square goes dark although no vertex
// moved.
func TestMorphTargetTurnsNormals(t *testing.T) {
	r := newMorphTestRenderer(t)
	scene := scenes.New()
	defer scene.Destroy()
	scene.SetAmbient(colors.RGB32F{0, 0, 0}, 0)
	scene.AddDirectionalLight(glm.Vec3f{0, 0, 1}, colors.RGB32F{1, 1, 1}, 1)

	config := morphSquare()
	front := glm.Vec3f{0, 0, 1}
	config.Attributes = append(config.Attributes,
		geometries.NewAttribute(geometries.AttributeNormal, geometries.Float32x3, []glm.Vec3f{front, front, front, front}))
	back := glm.Vec3f{0, 0, -2}
	config.MorphTargets = []geometries.MorphTarget{{
		Name:           "face away",
		PositionDeltas: make([]glm.Vec3f, 4),
		NormalDeltas:   []glm.Vec3f{back, back, back, back},
	}}
	geo := r.GeometryStore.Create(config)
	defer geo.Release()
	mat := r.NewPBRMaterial()
	defer mat.Release()
	mat.SetColor(colors.RGBA32F{1, 1, 1, 1})
	mat.SetRoughness(1)
	mat.SetCull(materials.CullNone)
	mesh := scene.NewMesh(geo, mat)
	addMorphTestCamera(scene)

	r.Render(scene)
	facing := r.Capture()[(morphViewSize/2*morphViewSize+morphViewSize/2)*4]
	mesh.SetMorphTargetWeight(0, 1)
	r.Render(scene)
	turned := r.Capture()[(morphViewSize/2*morphViewSize+morphViewSize/2)*4]

	if facing < 64 {
		t.Fatalf("square facing the light shades %d, too dark to compare against", facing)
	}
	if turned > facing/4 {
		t.Errorf("square with its normal morphed away from the light shades %d, want under a quarter of %d", turned, facing)
	}
}
