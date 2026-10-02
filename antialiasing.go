package pix

// AntiAliasing is how the renderer smooths the stair-steps along edges (see
// Renderer.SetAntiAliasing). It takes effect only while anti-aliasing is enabled
// (Renderer.EnableAntiAliasing).
type AntiAliasing uint8

const (
	// AntiAliasingMSAA4x samples coverage and depth four times per pixel while drawing
	// (multisample anti-aliasing), and averages them once the scene is drawn. Geometry
	// edges come out exactly smoothed; shading still runs once per pixel, so detail
	// inside a surface — sparkling highlights, alpha-tested foliage — is not.
	//
	// The default, and the method to prefer on mobile GPUs: they keep the samples in
	// on-chip memory, which makes them close to free.
	AntiAliasingMSAA4x AntiAliasing = iota
	// AntiAliasingMSAA2x is AntiAliasingMSAA4x with two samples: half the memory, with
	// coarser gradations along an edge.
	AntiAliasingMSAA2x
	// AntiAliasingFXAA smooths the finished frame in one full-screen pass after tone
	// mapping (fast approximate anti-aliasing): it finds sharp changes in brightness
	// and blends along them. It reaches every edge MSAA does not — highlights, foliage,
	// shader detail — at the cost of slightly softening textures. One pass, whatever
	// the scene, so its cost is fixed by the resolution.
	AntiAliasingFXAA
)

// antiAliasingNames is the console/round-trip spelling of each method, in enum order.
var antiAliasingNames = [...]string{"msaa4x", "msaa2x", "fxaa"}

// String returns the method's name ("msaa4x", "msaa2x", "fxaa").
func (a AntiAliasing) String() string {
	if int(a) < len(antiAliasingNames) {
		return antiAliasingNames[a]
	}
	return antiAliasingNames[AntiAliasingMSAA4x]
}

// ParseAntiAliasing resolves a method by name. The second result reports whether the
// name was known.
func ParseAntiAliasing(name string) (AntiAliasing, bool) {
	for i, known := range antiAliasingNames {
		if known == name {
			return AntiAliasing(i), true
		}
	}
	return AntiAliasingMSAA4x, false
}

// AntiAliasingNames lists every method's name, in enum order.
func AntiAliasingNames() []string {
	return antiAliasingNames[:]
}

// msaaSamples is how many samples per pixel the method draws with: one for a method
// that is not multisampled.
func (a AntiAliasing) msaaSamples() uint8 {
	switch a {
	case AntiAliasingMSAA4x:
		return 4
	case AntiAliasingMSAA2x:
		return 2
	default:
		return 1
	}
}
