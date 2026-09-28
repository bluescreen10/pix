package pix

import (
	"github.com/bluescreen10/pix/colors"
	"github.com/bluescreen10/pix/shaders"
)

// DebugView draws the scene with a dedicated fragment shader in place of every
// material's own, showing one property of the geometry instead of its shading — the
// "what is the geometry pass actually producing?" question, answered without a
// graphics debugger.
//
// Every view is a real geometry pass, not a re-read of a stored target. That is what
// makes them uniform: each reads only what the shared vertex stage provides, so a view
// behaves identically for a Basic, Blinn-Phong, PBR or custom material, and shows
// geometry no earlier pass happened to write.
//
// These used to be re-reads of the G-buffer's own targets, which meant they applied
// only while deferred rendering was on and only to materials that had a deferred path;
// anything forward-only was simply missing from them. The renderer is forward-only
// now, and with it went the albedo, material and emissive views — those read a
// material record, whose layout belongs to the material type, and no single shader can
// decode all of them.
type DebugView uint32

const (
	DebugOff        DebugView = iota // shade normally
	DebugNormal                      // world normals, decoded and remapped to [0,1]
	DebugDepth                       // depth, curved for readability: near dark, far bright
	DebugPosition                    // world position, fractional, so the scene reads as a unit grid
	DebugObjectID                    // one flat color per drawable, from a small palette
	DebugTriangleID                  // one flat color per triangle, from the same palette

	debugViewCount
)

// debugViewNames is the console/round-trip spelling of each view, in enum order.
var debugViewNames = [...]string{"off", "normal", "depth", "position", "objectid", "triangleid"}

// String returns the view's name ("off", "normal", …).
func (v DebugView) String() string {
	if int(v) < len(debugViewNames) {
		return debugViewNames[v]
	}
	return "off"
}

// ParseDebugView resolves a view by name. The second result reports whether the name
// was known.
func ParseDebugView(s string) (DebugView, bool) {
	for i, name := range debugViewNames {
		if name == s {
			return DebugView(i), true
		}
	}
	return DebugOff, false
}

// DebugViewNames lists every accepted name, for help text and completion.
func DebugViewNames() []string {
	return debugViewNames[:]
}

// debugFragment is the dedicated fragment shader for the active view.
func debugFragment(v DebugView) []byte {
	switch v {
	case DebugNormal:
		return shaders.SceneDebugNormal
	case DebugDepth:
		return shaders.SceneDebugDepth
	case DebugPosition:
		return shaders.SceneDebugPosition
	case DebugObjectID:
		return shaders.SceneDebugObject
	default:
		return shaders.SceneDebugTriangle
	}
}

// debugClear is what a view's pass clears to, which is not always the scene's own
// clear colour: a view paints a quantity, so its background has to be a value in the
// same scale rather than whatever the sky happens to be.
//
// Depth is the one that matters. Near reads dark and far reads bright, so background —
// nothing drawn, which is as far as it gets — has to be white; leaving it at the scene
// clear puts it at the near end of the ramp, where it is indistinguishable from
// geometry pressed against the camera. The old fullscreen pass got this for free by
// shading every pixel, including the ones no geometry covered.
func debugClear(v DebugView, sceneClear colors.RGBA32F) colors.RGBA32F {
	if v == DebugDepth {
		return colors.RGBA32F{1, 1, 1, 1}
	}
	return sceneClear
}
