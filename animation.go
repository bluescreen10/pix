package pix

import "github.com/bluescreen10/pix/glm"

// Channel is a Track's animated property.
type Channel uint8

const (
	ChannelPosition Channel = iota
	ChannelRotation
	ChannelScale
)

// Interp is a Track's keyframe interpolation mode.
type Interp uint8

const (
	InterpLinear Interp = iota
	InterpStep
)

// Track animates one property of one scene node over time. Target is bound
// directly to a node handle rather than a name — a loader that builds an
// AnimationClip (see loaders/gltf) already has the node in hand from building the
// scene graph, so there is no name-based re-targeting step (yet) between a clip
// and the mixer that plays it. Values is 3 floats per key for Position/Scale, 4
// (a quaternion, xyzw) for Rotation.
type Track struct {
	Target  SceneNode
	Channel Channel
	Interp  Interp
	Times   []float32
	Values  []float32
}

// AnimationClip is an immutable, shareable set of tracks with a duration — build
// once (or load once, e.g. via loaders/gltf), then play on as many
// AnimationMixers as you like via AnimationMixer.Action.
type AnimationClip struct {
	Name     string
	Duration float32
	Tracks   []Track
}

// keyAt finds the keyframe segment containing t, searching forward from *cursor
// (the caller resets it to 0 whenever time jumps backward — e.g. a loop wrap —
// since this search only ever advances). Returns the earlier key's index and the
// fractional position toward the next key (0 = exactly on the earlier key, or the
// track has no next key to interpolate toward).
func (tr *Track) keyAt(t float32, cursor *int) (int, float32) {
	n := len(tr.Times)
	if n == 0 {
		return 0, 0
	}
	if *cursor >= n {
		*cursor = n - 1
	}
	for *cursor > 0 && tr.Times[*cursor] > t {
		*cursor--
	}
	for *cursor+1 < n && tr.Times[*cursor+1] <= t {
		*cursor++
	}
	i := *cursor
	if i+1 >= n {
		return i, 0
	}
	span := tr.Times[i+1] - tr.Times[i]
	if span <= 0 {
		return i, 0
	}
	frac := (t - tr.Times[i]) / span
	if frac < 0 {
		frac = 0
	} else if frac > 1 {
		frac = 1
	}
	return i, frac
}

// sampleVec3 evaluates a Position/Scale track at time t.
func (tr *Track) sampleVec3(t float32, cursor *int) glm.Vec3f {
	i, frac := tr.keyAt(t, cursor)
	a := glm.Vec3f{tr.Values[i*3], tr.Values[i*3+1], tr.Values[i*3+2]}
	if frac == 0 || tr.Interp == InterpStep || (i+1)*3+2 >= len(tr.Values) {
		return a
	}
	b := glm.Vec3f{tr.Values[(i+1)*3], tr.Values[(i+1)*3+1], tr.Values[(i+1)*3+2]}
	return glm.Vec3f{
		a[0] + (b[0]-a[0])*frac,
		a[1] + (b[1]-a[1])*frac,
		a[2] + (b[2]-a[2])*frac,
	}
}

// sampleQuat evaluates a Rotation track at time t.
func (tr *Track) sampleQuat(t float32, cursor *int) glm.Quatf {
	i, frac := tr.keyAt(t, cursor)
	a := glm.Quatf{tr.Values[i*4], tr.Values[i*4+1], tr.Values[i*4+2], tr.Values[i*4+3]}
	if frac == 0 || tr.Interp == InterpStep || (i+1)*4+3 >= len(tr.Values) {
		return a
	}
	b := glm.Quatf{tr.Values[(i+1)*4], tr.Values[(i+1)*4+1], tr.Values[(i+1)*4+2], tr.Values[(i+1)*4+3]}
	return glm.Slerp(a, b, frac)
}
