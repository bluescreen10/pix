package pix

import "github.com/bluescreen10/pix/glm"

// LoopMode selects how an AnimationAction's local time behaves once it reaches
// the end (or start, if running backward) of its clip.
type LoopMode uint8

const (
	LoopRepeat   LoopMode = iota // wraps back to the start (or end, running backward)
	LoopOnce                     // clamps at the end (or start) and stops
	LoopPingPong                 // bounces back and forth between start and end
)

// AnimationAction plays one AnimationClip on an AnimationMixer: its own local
// time, playback rate, and blend weight (for blending against other actions that
// touch the same nodes this frame). Method chaining mirrors three.js's
// AnimationAction API. FadeIn/FadeOut/CrossFadeTo and finished callbacks are not
// implemented yet — drive Weight yourself for now if you need a blend.
type AnimationAction struct {
	clip      *AnimationClip
	time      float32
	weight    float32
	timeScale float32
	loop      LoopMode
	playing   bool
	paused    bool
	forward   bool // current direction (LoopPingPong flips this)

	cursors []int // per-track keyframe search cursor, reused across Update calls
}

func newAnimationAction(clip *AnimationClip) *AnimationAction {
	return &AnimationAction{
		clip: clip, weight: 1, timeScale: 1, forward: true,
		cursors: make([]int, len(clip.Tracks)),
	}
}

// Play starts (or resumes) playback.
func (a *AnimationAction) Play() *AnimationAction { a.playing = true; return a }

// Stop halts playback and resets local time to 0.
func (a *AnimationAction) Stop() *AnimationAction {
	a.playing = false
	a.time = 0
	a.resetCursors()
	return a
}

// Reset rewinds local time to 0 without stopping.
func (a *AnimationAction) Reset() *AnimationAction {
	a.time = 0
	a.resetCursors()
	return a
}

// SetWeight sets the action's blend weight (0 = no contribution).
func (a *AnimationAction) SetWeight(w float32) *AnimationAction { a.weight = w; return a }

// SetTimeScale sets the playback rate (negative plays backward).
func (a *AnimationAction) SetTimeScale(s float32) *AnimationAction { a.timeScale = s; return a }

// SetLoop sets how local time behaves past the clip's duration.
func (a *AnimationAction) SetLoop(mode LoopMode) *AnimationAction { a.loop = mode; return a }

// SetPaused freezes (or resumes) local time without stopping — a paused action
// still contributes its current pose to the blend.
func (a *AnimationAction) SetPaused(p bool) *AnimationAction { a.paused = p; return a }

// SetTime sets local time directly.
func (a *AnimationAction) SetTime(t float32) *AnimationAction {
	a.time = t
	a.resetCursors()
	return a
}

func (a *AnimationAction) Time() float32   { return a.time }
func (a *AnimationAction) Weight() float32 { return a.weight }
func (a *AnimationAction) IsPlaying() bool { return a.playing && !a.paused }

func (a *AnimationAction) resetCursors() {
	for i := range a.cursors {
		a.cursors[i] = 0
	}
}

// advance moves local time forward by dt*timeScale and applies the loop mode.
func (a *AnimationAction) advance(dt float32) {
	d := a.clip.Duration
	if d <= 0 {
		return
	}
	step := dt * a.timeScale
	if !a.forward {
		step = -step
	}
	a.time += step
	switch a.loop {
	case LoopOnce:
		if a.time >= d {
			a.time = d
			a.playing = false
		} else if a.time < 0 {
			a.time = 0
			a.playing = false
		}
	case LoopPingPong:
		for a.time > d || a.time < 0 {
			if a.time > d {
				a.time = 2*d - a.time
				a.forward = false
			} else {
				a.time = -a.time
				a.forward = true
			}
			a.resetCursors()
		}
	default: // LoopRepeat
		if a.time >= d {
			a.time -= d
			a.resetCursors()
		} else if a.time < 0 {
			a.time += d
			a.resetCursors()
		}
	}
}

// mixAccum is one node's per-frame blend accumulator: position/scale as a
// weighted sum, rotation as an incremental weighted slerp (three.js's approach).
type mixAccum struct {
	node             NodeID
	posSum, scaleSum glm.Vec3f
	posW, scaleW     float32
	rot              glm.Quatf
	rotW             float32
}

// AnimationMixer plays AnimationActions against a scene's node transforms,
// blending when more than one action touches the same node in the same frame.
// Tracks are bound directly to node handles by whatever built the clip (e.g.
// loaders/gltf) — there is no name-based re-targeting between a clip and a mixer
// yet, so root is currently unused beyond documenting intent for that.
type AnimationMixer struct {
	scene   *Scene
	actions []*AnimationAction
	// accum entries persist across frames, keyed by node slot, and are reused in
	// place (fields zeroed, not the map entry deleted) — a mixer driving a fixed
	// skeleton settles into zero allocations per Update once every node it
	// touches has been seen once.
	accum map[uint32]*mixAccum
}

// NewAnimationMixer creates a mixer that will drive nodes in this scene.
func (s *Scene) NewAnimationMixer(root SceneNode) *AnimationMixer {
	_ = root
	return &AnimationMixer{scene: s, accum: map[uint32]*mixAccum{}}
}

// Action returns a new AnimationAction playing clip on this mixer (stopped, at
// weight 1, until Play is called).
func (m *AnimationMixer) Action(clip *AnimationClip) *AnimationAction {
	a := newAnimationAction(clip)
	m.actions = append(m.actions, a)
	return a
}

// StopAll stops every action this mixer owns.
func (m *AnimationMixer) StopAll() {
	for _, a := range m.actions {
		a.Stop()
	}
}

// Update advances every playing action's local time by dt, blends their tracks
// per touched node (weighted sum for position/scale, weighted slerp for
// rotation), and applies the result via SetPosition/SetRotationQuat/SetScale —
// which is what marks the node dirty for the next UpdateTransforms. A node with
// zero total weight this frame is left untouched (it keeps its last-applied
// value rather than reverting to bind pose — a v1 simplification).
func (m *AnimationMixer) Update(dt float32) {
	// Snapshot which actions are active before advancing: a LoopOnce action that
	// reaches its end this call flips playing=false inside advance(), but its
	// final (clamped) pose must still be applied this frame — checking a.playing
	// again below would silently skip it.
	active := m.actions[:0:0] // fresh backing array; m.actions itself is untouched
	for _, a := range m.actions {
		if !a.playing {
			continue
		}
		active = append(active, a)
		if !a.paused {
			a.advance(dt)
		}
	}

	// Zero every accumulator this mixer has ever touched — cheap field writes,
	// no allocation, unlike deleting and re-inserting map entries every frame.
	for _, en := range m.accum {
		en.posSum, en.scaleSum, en.posW, en.scaleW, en.rotW = glm.Vec3f{}, glm.Vec3f{}, 0, 0, 0
	}

	for _, a := range active {
		if a.weight <= 0 {
			continue
		}
		w := a.weight
		for i := range a.clip.Tracks {
			tr := &a.clip.Tracks[i]
			id := tr.Target.ID()
			en, ok := m.accum[id.index]
			if !ok {
				en = &mixAccum{node: id}
				m.accum[id.index] = en
			}
			switch tr.Channel {
			case ChannelPosition:
				v := tr.sampleVec3(a.time, &a.cursors[i])
				en.posSum = en.posSum.Add(v.Scale(w))
				en.posW += w
			case ChannelScale:
				v := tr.sampleVec3(a.time, &a.cursors[i])
				en.scaleSum = en.scaleSum.Add(v.Scale(w))
				en.scaleW += w
			case ChannelRotation:
				v := tr.sampleQuat(a.time, &a.cursors[i])
				if en.rotW == 0 {
					en.rot = v
				} else {
					en.rot = glm.Slerp(en.rot, v, w/(en.rotW+w))
				}
				en.rotW += w
			}
		}
	}

	for _, en := range m.accum {
		n := Node{scene: m.scene, id: en.node}
		if en.posW > 0 {
			n.SetPosition(en.posSum.Scale(1 / en.posW))
		}
		if en.scaleW > 0 {
			n.SetScale(en.scaleSum.Scale(1 / en.scaleW))
		}
		if en.rotW > 0 {
			n.SetRotationQuat(en.rot)
		}
	}
}
