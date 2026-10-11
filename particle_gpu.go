// The GPU-facing half of particle systems: the push-constant roots the simulation and
// draw shaders read, and the cube-face basis point-light shadows are rendered with.
// The systems themselves are scene state (see scenes.ParticleContainer); these are the
// shapes this renderer chose to drive them with, which is why they live here.
package pix

import (
	"unsafe"

	"github.com/bluescreen10/gamekit/gpu"
	"github.com/bluescreen10/gamekit/utils"
	"github.com/bluescreen10/pix/glm"
	"github.com/bluescreen10/pix/scenes"
)

var particleRecordSize = uint32(unsafe.Sizeof(scenes.ParticleRecord{}))

// particleSimulationRoot matches PC in shaders/lib/particles/simulation.glsl up to
// PARTICLE_PARAMS: 48 bytes, after which an update shader's own parameters follow. One
// per container per frame — see encodeParticleSimulation.
type particleSimulationRoot struct {
	source, destination, newborns, draw uint64
	capacity, newbornCount              uint32
	dt                                  float32
	_                                   uint32
}

// appendWithParams appends the root to buf, followed by an update shader's params, and
// pads the whole to a multiple of 16 bytes for the same reason drawable.go's *Root types
// are: MSL rounds a struct's size up to its alignment, and a mismatch there makes the
// Metal backend hand the shader a short root.
func (root *particleSimulationRoot) appendWithParams(buf, params []byte) []byte {
	buf = append(buf, utils.ToBytes(root)...)
	buf = append(buf, params...)
	if remainder := len(buf) % 16; remainder != 0 {
		buf = append(buf, make([]byte, 16-remainder)...)
	}
	return buf
}

// defaultParticleParams matches PARTICLE_PARAMS in particle_update.comp.glsl, pix's
// update for a ParticleUpdate with no Shader of its own.
type defaultParticleParams struct {
	gravity                  glm.Vec3f
	drag                     float32
	sizeEnabled              uint32
	sizeStart, sizeEnd       float32
	opacityEnabled           uint32
	opacityStart, opacityEnd float32
}

func defaultParticleParamsFor(update scenes.ParticleUpdate) defaultParticleParams {
	params := defaultParticleParams{
		gravity: update.Gravity,
		drag:    update.Drag,
	}
	if update.SizeOverLife.Enabled {
		params.sizeEnabled, params.sizeStart, params.sizeEnd = 1, update.SizeOverLife.Start, update.SizeOverLife.End
	}
	if update.OpacityOverLife.Enabled {
		params.opacityEnabled, params.opacityStart, params.opacityEnd = 1, update.OpacityOverLife.Start, update.OpacityOverLife.End
	}
	return params
}

// particleUpdatePipeline is the compute pipeline built from one particle update shader.
type particleUpdatePipeline struct {
	shader   []byte
	pipeline gpu.Pipeline
}

// particleDrawRoot matches PC in particle_common.glsl / particle_draw.vert.glsl
// (scalar; mat4, then pointers, then plain fields). Particles have their own
// push-constant contract, not a reuse of drawRoot: geometryID/materialID/
// transformID are named fields here (a container has exactly one of each for the
// whole draw), and the fragment side's vColor carries the particle's real color+
// alpha — see particle_common.glsl's comment on why that couldn't ride the mesh
// path's shared contract. One per container per frame — see drawParticles.
type particleDrawRoot struct {
	viewProj               glm.Mat4f
	pos, attr, descs       uint64
	models, particles      uint64
	materials, lights      uint64
	eye                    glm.Vec4f
	geometryID, materialID uint32
	transformID            uint32
	// time is elapsed seconds since the scene's clock started (Scene.clockStart) —
	// passed unconditionally, same as drawRoot's (drawable.go); a particle shader
	// reads it or ignores it.
	time float32
	// order is a sorted container's back-to-front order, or 0 to draw its particles in
	// compaction order.
	order uint64
}

var particleDrawRootSize = uint32(unsafe.Sizeof(particleDrawRoot{}))

// particleSortKeysRoot matches PC in particle_sort_keys.comp.glsl: one per sorted
// container per frame — see encodeParticleSorting.
type particleSortKeysRoot struct {
	particles, indirect, models, order uint64
	// depthPlane gives a point's view depth: dot(depthPlane.xyz, p) + depthPlane.w.
	depthPlane  glm.Vec4f
	transformID uint32
	// count is the entries in order: the capacity rounded up to a power of two.
	count      uint32
	pad0, pad1 uint32
}

// particleSortStepRoot matches PC in particle_sort_step.comp.glsl: one per step of
// the bitonic sort.
type particleSortStepRoot struct {
	order                             uint64
	count, blockSize, partnerDistance uint32
	pad0, pad1, pad2                  uint32
}

// sortEntrySize is the size of one entry of a sorted container's order buffer: a
// float key and a particle index.
const sortEntrySize = 8

// cubeFaceDirs/cubeFaceUps are the six 90° cube-face view directions and their up
// vectors (the ±Y faces use a Z up to avoid a degenerate look-at). Face order matches
// the shader's dominant-axis selection: +X,-X,+Y,-Y,+Z,-Z.
var cubeFaceDirs = [6]glm.Vec3f{{1, 0, 0}, {-1, 0, 0}, {0, 1, 0}, {0, -1, 0}, {0, 0, 1}, {0, 0, -1}}
var cubeFaceUps = [6]glm.Vec3f{{0, 1, 0}, {0, 1, 0}, {0, 0, 1}, {0, 0, -1}, {0, 1, 0}, {0, 1, 0}}
