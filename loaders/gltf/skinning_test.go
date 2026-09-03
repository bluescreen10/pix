package gltf

import (
	"testing"

	"github.com/bluescreen10/pix"
	"github.com/bluescreen10/pix/cameras"
	_ "github.com/bluescreen10/pix/gpu/vulkan"
)

const capoeiraAsset = "../../examples/skinning/assets/capoeira.gltf"

// TestLoadSkinnedAnimatedAsset loads the capoeira example asset (a Mixamo rig:
// 65 joints, 1 skin, 2 skinned mesh primitives, 1 animation clip with 195
// tracks) end to end: skeleton built, skinned meshes attached to it, one
// animation clip parsed, and — the actual point of the skinning pipeline — an
// AnimationMixer pose actually changes a bone's world transform frame to frame.
func TestLoadSkinnedAnimatedAsset(t *testing.T) {
	r, err := pix.NewOffscreenRenderer(64, 64)
	if err != nil {
		t.Fatal(err)
	}
	defer r.Destroy()
	scene := r.NewScene()
	defer scene.Destroy()

	res, err := LoadFull(r, scene, capoeiraAsset)
	if err != nil {
		t.Fatal(err)
	}
	if res.Added == 0 {
		t.Fatal("no mesh nodes added")
	}
	if len(res.Skeletons) != 1 {
		t.Fatalf("skeletons = %d, want 1", len(res.Skeletons))
	}
	skel := res.Skeletons[0]
	if n := skel.BoneCount(); n != 65 {
		t.Fatalf("bone count = %d, want 65", n)
	}
	if b := skel.BoneByName("mixamorig:Hips"); !b.IsValid() {
		t.Fatal("BoneByName(\"mixamorig:Hips\") not found")
	}
	if len(res.Clips) != 1 {
		t.Fatalf("clips = %d, want 1", len(res.Clips))
	}
	clip := res.Clips[0]
	if len(clip.Tracks) != 195 {
		t.Fatalf("tracks = %d, want 195 (65 joints x 3 channels)", len(clip.Tracks))
	}
	if clip.Duration <= 0 {
		t.Fatal("clip duration <= 0")
	}
	t.Logf("clip %q: %d tracks, duration %.2fs", clip.Name, len(clip.Tracks), clip.Duration)

	// Render once so the skeleton's Sync-computed bind-pose bounds exist, then
	// frame it — an all-SkinnedMesh scene must still be frameable (FrameSphere
	// has to see skinnedMeshes, not just meshes).
	cam := cameras.NewPerspectiveCamera(45, 1, 1, 1000)
	r.Render(scene, cam)
	center, radius := scene.FrameSphere(0.9)
	if radius <= 0 {
		t.Fatalf("FrameSphere radius = %v, want > 0 (scene should not look empty)", radius)
	}
	t.Logf("frame sphere: center=%v radius=%v", center, radius)

	// Drive the mixer and confirm a bone's world transform actually changes —
	// proof the clip's tracks are correctly bound and sampled, not just parsed.
	hips := skel.BoneByName("mixamorig:Hips")
	mixer := scene.NewAnimationMixer(skel)
	action := mixer.Action(clip)
	action.SetLoop(pix.LoopRepeat).Play()

	scene.UpdateTransforms()
	before := hips.WorldTransform()

	mixer.Update(0.5)
	scene.UpdateTransforms()
	after := hips.WorldTransform()

	if before == after {
		t.Fatal("hips world transform did not change after mixer.Update — animation not driving the skeleton")
	}
}
