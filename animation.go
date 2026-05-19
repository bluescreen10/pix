package pix

import "github.com/bluescreen10/pix/glm"

// Interpolation mode for keyframe tracks.
type Interpolation int

const (
	InterpolationLinear Interpolation = iota
	InterpolationStep
)

// PositionTrack animates a node's local position.
type PositionTrack struct {
	Target Node
	Times  []float32
	Values []glm.Vec3f
	Mode   Interpolation
}

// RotationTrack animates a node's local rotation (quaternion slerp).
type RotationTrack struct {
	Target Node
	Times  []float32
	Values []glm.Quatf
	Mode   Interpolation
}

// ScaleTrack animates a node's local scale.
type ScaleTrack struct {
	Target Node
	Times  []float32
	Values []glm.Vec3f
	Mode   Interpolation
}

// AnimationClip is a named set of tracks with a fixed duration.
type AnimationClip struct {
	Name      string
	Duration  float32
	Positions []PositionTrack
	Rotations []RotationTrack
	Scales    []ScaleTrack
}

// LoopMode controls how an action repeats.
type LoopMode int

const (
	LoopOnce     LoopMode = iota // stop at end
	LoopRepeat                   // wrap back to start
	LoopPingPong                 // reverse direction at each end
)

// AnimationAction is an active playback of a clip. Obtain one via Mixer.ClipAction.
type AnimationAction struct {
	Clip      *AnimationClip
	Loop      LoopMode
	TimeScale float32
	Weight    float32 // blending weight (0–1); currently applied as a scalar on track values

	time    float32
	dir     float32 // +1 or -1 for ping-pong
	active  bool
}

// Play starts or resumes the action. Returns itself for chaining.
func (a *AnimationAction) Play() *AnimationAction { a.active = true; return a }

// Stop pauses the action without resetting its time.
func (a *AnimationAction) Stop() *AnimationAction { a.active = false; return a }

// Reset rewinds the action to time 0.
func (a *AnimationAction) Reset() *AnimationAction { a.time = 0; a.dir = 1; return a }

// IsPlaying reports whether the action is currently advancing.
func (a *AnimationAction) IsPlaying() bool { return a.active }

// AnimationMixer manages a set of AnimationActions and advances them each frame.
type AnimationMixer struct {
	actions []*AnimationAction
}

func NewAnimationMixer() *AnimationMixer { return &AnimationMixer{} }

// ClipAction returns an existing action for the given clip, or creates a new one.
func (m *AnimationMixer) ClipAction(clip *AnimationClip) *AnimationAction {
	for _, a := range m.actions {
		if a.Clip == clip {
			return a
		}
	}
	a := &AnimationAction{Clip: clip, Loop: LoopRepeat, TimeScale: 1, Weight: 1, dir: 1}
	m.actions = append(m.actions, a)
	return a
}

// Update advances all active actions by dt seconds and applies track values to
// their target nodes.
func (m *AnimationMixer) Update(dt float32) {
	for _, a := range m.actions {
		if !a.active {
			continue
		}
		clip := a.Clip

		// Advance time.
		a.time += dt * a.TimeScale * a.dir

		switch a.Loop {
		case LoopOnce:
			if a.time >= clip.Duration {
				a.time = clip.Duration
				a.active = false
			} else if a.time < 0 {
				a.time = 0
				a.active = false
			}
		case LoopRepeat:
			if clip.Duration > 0 {
				for a.time >= clip.Duration {
					a.time -= clip.Duration
				}
				for a.time < 0 {
					a.time += clip.Duration
				}
			}
		case LoopPingPong:
			if a.time >= clip.Duration {
				a.time = clip.Duration - (a.time - clip.Duration)
				a.dir = -1
			} else if a.time < 0 {
				a.time = -a.time
				a.dir = 1
			}
		}

		// Apply tracks.
		for i := range clip.Positions {
			t := &clip.Positions[i]
			v := sampleVec3(t.Times, t.Values, a.time, t.Mode)
			t.Target.SetPosition(v)
		}
		for i := range clip.Rotations {
			t := &clip.Rotations[i]
			v := sampleQuat(t.Times, t.Values, a.time, t.Mode)
			t.Target.SetRotationQuat(v)
		}
		for i := range clip.Scales {
			t := &clip.Scales[i]
			v := sampleVec3(t.Times, t.Values, a.time, t.Mode)
			t.Target.SetScale(v)
		}
	}
}

// sampleVec3 samples a []Vec3f keyframe track at time t.
func sampleVec3(times []float32, values []glm.Vec3f, t float32, mode Interpolation) glm.Vec3f {
	if len(times) == 0 {
		return glm.Vec3f{}
	}
	if t <= times[0] {
		return values[0]
	}
	last := len(times) - 1
	if t >= times[last] {
		return values[last]
	}
	// Binary search for the surrounding keyframes.
	lo, hi := 0, last
	for lo+1 < hi {
		mid := (lo + hi) / 2
		if times[mid] <= t {
			lo = mid
		} else {
			hi = mid
		}
	}
	if mode == InterpolationStep {
		return values[lo]
	}
	alpha := (t - times[lo]) / (times[hi] - times[lo])
	a, b := values[lo], values[hi]
	return glm.Vec3f{
		a[0] + alpha*(b[0]-a[0]),
		a[1] + alpha*(b[1]-a[1]),
		a[2] + alpha*(b[2]-a[2]),
	}
}

// sampleQuat samples a []Quatf keyframe track at time t using slerp.
func sampleQuat(times []float32, values []glm.Quatf, t float32, mode Interpolation) glm.Quatf {
	if len(times) == 0 {
		return glm.QuatIdentityf
	}
	if t <= times[0] {
		return values[0]
	}
	last := len(times) - 1
	if t >= times[last] {
		return values[last]
	}
	lo, hi := 0, last
	for lo+1 < hi {
		mid := (lo + hi) / 2
		if times[mid] <= t {
			lo = mid
		} else {
			hi = mid
		}
	}
	if mode == InterpolationStep {
		return values[lo]
	}
	alpha := (t - times[lo]) / (times[hi] - times[lo])
	return glm.Slerp(values[lo], values[hi], alpha)
}
