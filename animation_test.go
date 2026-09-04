package pix

import "testing"

// TestAnimationMixerDrivesNode plays a simple 2-key position track on a group
// node and checks the mixer's Update actually applies interpolated positions —
// headless, no GPU involved.
func TestAnimationMixerDrivesNode(t *testing.T) {
	r, err := NewOffscreenRenderer(4, 4)
	if err != nil {
		t.Fatal(err)
	}
	defer r.Destroy()
	scene := r.NewScene()
	defer scene.Destroy()

	n := scene.NewGroup()
	scene.Add(n)

	clip := &AnimationClip{
		Name:     "move",
		Duration: 2,
		Tracks: []Track{
			{
				Target:  n,
				Channel: ChannelPosition,
				Interp:  InterpLinear,
				Times:   []float32{0, 2},
				Values:  []float32{0, 0, 0, 10, 0, 0},
			},
		},
	}

	mixer := scene.NewAnimationMixer(n)
	action := mixer.Action(clip)
	action.SetLoop(LoopOnce).Play()

	mixer.Update(1) // halfway through the 2-second track
	scene.Sync()
	pos := n.Position()
	if pos[0] < 4.9 || pos[0] > 5.1 {
		t.Fatalf("position.X at t=1 = %v, want ~5 (halfway through 0->10)", pos[0])
	}

	mixer.Update(5) // run well past the end; LoopOnce should clamp and stop
	scene.Sync()
	pos = n.Position()
	if pos[0] < 9.9 || pos[0] > 10.1 {
		t.Fatalf("position.X after overshoot = %v, want ~10 (clamped)", pos[0])
	}
	if action.IsPlaying() {
		t.Fatal("LoopOnce action should have stopped after reaching the end")
	}
}

// TestAnimationMixerLoopRepeat checks that LoopRepeat wraps time rather than
// clamping.
func TestAnimationMixerLoopRepeat(t *testing.T) {
	r, err := NewOffscreenRenderer(4, 4)
	if err != nil {
		t.Fatal(err)
	}
	defer r.Destroy()
	scene := r.NewScene()
	defer scene.Destroy()

	n := scene.NewGroup()
	scene.Add(n)

	clip := &AnimationClip{
		Duration: 1,
		Tracks: []Track{
			{
				Target: n, Channel: ChannelPosition, Interp: InterpLinear,
				Times: []float32{0, 1}, Values: []float32{0, 0, 0, 10, 0, 0},
			},
		},
	}
	mixer := scene.NewAnimationMixer(n)
	action := mixer.Action(clip)
	action.SetLoop(LoopRepeat).Play()

	mixer.Update(1.25) // wraps: 1.25 mod 1 = 0.25
	scene.Sync()
	pos := n.Position()
	if pos[0] < 2.0 || pos[0] > 3.0 {
		t.Fatalf("position.X after wrap = %v, want ~2.5 (25%% through the loop)", pos[0])
	}
	if !action.IsPlaying() {
		t.Fatal("LoopRepeat action should still be playing")
	}
}

// TestAnimationMixerBlendsTwoActions checks that two actions on the same node,
// weighted 0.5/0.5, produce the midpoint between their two poses.
func TestAnimationMixerBlendsTwoActions(t *testing.T) {
	r, err := NewOffscreenRenderer(4, 4)
	if err != nil {
		t.Fatal(err)
	}
	defer r.Destroy()
	scene := r.NewScene()
	defer scene.Destroy()

	n := scene.NewGroup()
	scene.Add(n)

	still := func(x float32) *AnimationClip {
		return &AnimationClip{
			Duration: 1,
			Tracks: []Track{
				{Target: n, Channel: ChannelPosition, Interp: InterpStep, Times: []float32{0}, Values: []float32{x, 0, 0}},
			},
		}
	}
	mixer := scene.NewAnimationMixer(n)
	a := mixer.Action(still(0))
	b := mixer.Action(still(10))
	a.SetWeight(0.5).Play()
	b.SetWeight(0.5).Play()

	mixer.Update(0)
	scene.Sync()
	pos := n.Position()
	if pos[0] < 4.9 || pos[0] > 5.1 {
		t.Fatalf("blended position.X = %v, want ~5 (midpoint of 0 and 10 at equal weight)", pos[0])
	}
}
