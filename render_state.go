package pix

import (
	"github.com/bluescreen10/gamekit/gpu"
	"github.com/bluescreen10/pix/scenes"
)

// renderState is the GPU counterpart of a FramePacket: the same frame description, kept
// on the GPU and organized the way this renderer draws, for one packet source.
//
// Each frame the renderer compares the new packet against it and re-syncs whatever has
// gone stale (see Renderer.extract). It is deliberately plain data: it knows nothing of
// pipelines, fitting or ordering — every decision that fills it is the renderer's.
//
// It holds two kinds of thing. Most of it is a cache that the next packet could
// rebuild: the layout, the buffers mirroring it, the light table, the shadow maps. The
// particle simulations and the LOD hysteresis in prevLevelBuf are not: they accumulate
// across frames, and nothing in a packet can reconstruct them.
//
// It is keyed by SourceID rather than held by the Scene, so two scenes can never share
// buffers and a scene can be destroyed without reaching into a renderer.
type renderState struct {
	// layout is what to draw, rebuilt when it goes stale. The buffers below hold its
	// GPU-side mirror.
	layout drawLayout

	worldBuf     gpu.Buffer // world matrices: nodes, then instance transforms
	drawableBuf  gpu.Buffer // layout.drawables
	lodTableBuf  gpu.Buffer // layout.lods
	jointBuf     gpu.Buffer // skinning palettes, rewritten every frame
	prevLevelBuf gpu.Buffer // the LOD level each transform showed last frame; persists

	// Every view culls into its own buffers — the main camera and each shadow camera —
	// while sharing the drawable and world buffers above.
	mainCull    cullBuffers
	shadowCulls []cullBuffers

	// lights is the GPU light table; shadows the depth maps and cameras of the casting
	// lights, keyed by light identity.
	lights  *Lights
	shadows map[scenes.LightID]*shadowResource

	// particles is each particle system's simulation, keyed by its stable id.
	particles map[scenes.ParticleID]*particleState
}

// cullBuffers is what one view's cull writes: each batch's indirect arguments, with the
// instance count filled in, and the compacted list of drawables that survived.
type cullBuffers struct {
	indirectBuf gpu.Buffer
	visibleBuf  gpu.Buffer
}
