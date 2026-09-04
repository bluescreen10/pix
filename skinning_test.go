package pix

import (
	"testing"

	"github.com/bluescreen10/pix/cameras"
	"github.com/bluescreen10/pix/colors"
	"github.com/bluescreen10/pix/glm"
)

// riggedQuad returns a narrow 2-unit-tall quad in skeleton-local space, standing on
// the origin: its bottom two vertices are fully weighted to joint 0, its top two to
// joint 1. Meant to be paired with a two-bone skeleton whose joint 1 sits at (0,2,0).
func riggedQuad() GeometryConfig {
	positions := []glm.Vec3f{
		{-0.4, 0, 0}, {0.4, 0, 0}, // bottom (joint 0)
		{-0.4, 2, 0}, {0.4, 2, 0}, // top (joint 1)
	}
	joints := []glm.Vec4[uint16]{
		{0, 0, 0, 0}, {0, 0, 0, 0},
		{1, 0, 0, 0}, {1, 0, 0, 0},
	}
	weights := []glm.Vec4f{
		{1, 0, 0, 0}, {1, 0, 0, 0},
		{1, 0, 0, 0}, {1, 0, 0, 0},
	}
	return GeometryConfig{
		Attributes: []Attribute{
			NewAttribute(AttributePosition, Float32x3, positions),
			NewAttribute(AttributeSkinIndex, Uint16x4, joints),
			NewAttribute(AttributeSkinWeight, Float32x4, weights),
		},
		// Both windings, so the quad is visible regardless of which way it faces
		// after bending — this test cares about silhouette movement, not culling.
		Indices: []uint32{0, 1, 2, 2, 1, 3, 0, 2, 1, 2, 3, 1},
	}
}

// twoBoneSkeleton returns a SkeletonConfig for a 2-joint chain: joint 0 at the
// origin, joint 1 parented to it at local (0,2,0) — matching riggedQuad's bind pose.
func twoBoneSkeleton() SkeletonConfig {
	return SkeletonConfig{
		Names:   []string{"base", "tip"},
		Parents: []int32{-1, 0},
		InverseBind: []glm.Mat4f{
			glm.Mat4fIndentity,
			glm.Transform(glm.Vec3f{1, 1, 1}, glm.QuatIdentityf, glm.Vec3f{0, -2, 0}),
		},
		BindPose: []Transform{
			{Rotation: glm.QuatIdentityf, Scale: glm.Vec3f{1, 1, 1}},
			{Position: glm.Vec3f{0, 2, 0}, Rotation: glm.QuatIdentityf, Scale: glm.Vec3f{1, 1, 1}},
		},
	}
}

// countLitPixels counts pixels in an RGBA8 buffer brighter than the clear color
// (a crude silhouette measure — how much of the frame the mesh covers).
func countLitPixels(pixels []byte) int {
	n := 0
	for i := 0; i+3 < len(pixels); i += 4 {
		if pixels[i] > 10 || pixels[i+1] > 10 || pixels[i+2] > 10 {
			n++
		}
	}
	return n
}

// TestSkinnedMeshBindPose renders a rigged quad at bind pose and checks it produces
// a visible silhouette — i.e. the compute-skinning output landed at sane (bind-pose)
// positions rather than uninitialized garbage, and the draw pipeline can render a
// SkinnedMesh's output geometry at all.
func TestSkinnedMeshBindPose(t *testing.T) {
	r, err := NewOffscreenRenderer(64, 64)
	if err != nil {
		t.Fatal(err)
	}
	defer r.Destroy()
	r.SetClearColor(colors.RGBA32F{0, 0, 0, 1})

	scene := r.NewScene()
	defer scene.Destroy()

	geo := r.NewGeometry(riggedQuad())
	defer geo.Release()
	mat := r.NewBasicMaterial()
	mat.SetCull(CullNone)

	skel := scene.NewSkeleton(twoBoneSkeleton())
	scene.Add(skel)
	sm := scene.NewSkinnedMesh(geo, mat, skel)
	scene.Add(sm)

	cam := cameras.NewPerspectiveCamera(45, 1, 0.1, 100)
	cam.SetPosition(glm.Vec3f{0, 1, 6})

	r.Render(scene, cam)
	pixels := r.Capture()
	if pixels == nil {
		t.Fatal("Capture returned nil")
	}
	if n := countLitPixels(pixels); n == 0 {
		t.Fatal("bind-pose skinned quad produced no visible pixels — compute-skinning output not landing at sane positions")
	}
}

// TestSkinnedMeshDeforms bends the tip joint 90 degrees and checks the render
// actually changes relative to bind pose — proving the full compute-skin pipeline
// (dispatch, barrier, vertex read) picks up a pose change end to end.
func TestSkinnedMeshDeforms(t *testing.T) {
	r, err := NewOffscreenRenderer(64, 64)
	if err != nil {
		t.Fatal(err)
	}
	defer r.Destroy()
	r.SetClearColor(colors.RGBA32F{0, 0, 0, 1})

	scene := r.NewScene()
	defer scene.Destroy()

	geo := r.NewGeometry(riggedQuad())
	defer geo.Release()
	mat := r.NewBasicMaterial()
	mat.SetCull(CullNone)

	skel := scene.NewSkeleton(twoBoneSkeleton())
	scene.Add(skel)
	sm := scene.NewSkinnedMesh(geo, mat, skel)
	scene.Add(sm)

	cam := cameras.NewPerspectiveCamera(45, 1, 0.1, 100)
	cam.SetPosition(glm.Vec3f{0, 1, 6})

	r.Render(scene, cam)
	bindPixels := append([]byte(nil), r.Capture()...)

	// Bend the tip 90 degrees about Z: its vertices swing sideways out of the
	// silhouette the bind pose occupied.
	skel.Bone(1).RotateZ(glm.ToRadians(float32(90)))
	r.Render(scene, cam)
	bentPixels := r.Capture()

	diff := 0
	for i := range bindPixels {
		if bindPixels[i] != bentPixels[i] {
			diff++
		}
	}
	if diff == 0 {
		t.Fatal("bending joint 1 produced an identical frame — compute-skinning output did not update")
	}
	t.Logf("bind vs bent: %d/%d bytes differ", diff, len(bindPixels))
}

// TestSkinnedMeshBoneAttachment checks that a node parented to a bone follows it —
// bones being ordinary scene nodes is the whole point of the design (see
// skeleton.go's Bone type).
func TestSkinnedMeshBoneAttachment(t *testing.T) {
	r, err := NewOffscreenRenderer(4, 4)
	if err != nil {
		t.Fatal(err)
	}
	defer r.Destroy()
	scene := r.NewScene()
	defer scene.Destroy()

	skel := scene.NewSkeleton(twoBoneSkeleton())
	scene.Add(skel)

	prop := scene.NewGroup()
	skel.Bone(1).Add(prop)

	scene.UpdateTransforms()
	before := prop.WorldTransform()

	skel.Bone(1).SetPosition(glm.Vec3f{5, 2, 0})
	scene.UpdateTransforms()
	after := prop.WorldTransform()

	if before == after {
		t.Fatal("prop attached to bone 1 did not move when the bone moved")
	}
	if after[12] != 5 {
		t.Fatalf("prop world X = %v, want 5 (bone 1's new local X, since bone 0/root are at origin)", after[12])
	}
}
