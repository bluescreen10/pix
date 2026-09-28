// The GPU-facing half of particle systems: the push-constant roots the simulation and
// draw shaders read, and the cube-face basis point-light shadows are rendered with.
// The systems themselves are scene state (see scenes.ParticleContainer); these are the
// shapes this renderer chose to drive them with, which is why they live here.
package pix

import (
	"unsafe"

	"github.com/bluescreen10/pix/glm"
	"github.com/bluescreen10/pix/scenes"
)

var particleRecordSize = uint32(unsafe.Sizeof(scenes.ParticleRecord{}))

// particleUpdateRoot matches PC in particle_update.comp.glsl (scalar; pointers
// first, then plain fields). One per container per frame — see encodeParticleSimulation.
type particleUpdateRoot struct {
	src, dst, pending, indirect uint64
	capacity, pendingCount      uint32
	dt                          float32
	gravity                     glm.Vec3f
	drag                        float32
	sizeEnabled                 uint32
	sizeStart, sizeEnd          float32
	opacityEnabled              uint32
	opacityStart, opacityEnd    float32
	// Padded to a multiple of 16 for the same reason drawable.go's *Root types are:
	// MSL rounds a struct's size up to its alignment, and a mismatch there makes the
	// Metal backend hand the shader a short root.
	pad0, pad1, pad2 uint32
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
	pad0 uint32
}

var particleDrawRootSize = uint32(unsafe.Sizeof(particleDrawRoot{}))

var particleUpdateRootSize = uint32(unsafe.Sizeof(particleUpdateRoot{}))

// cubeFaceDirs/cubeFaceUps are the six 90° cube-face view directions and their up
// vectors (the ±Y faces use a Z up to avoid a degenerate look-at). Face order matches
// the shader's dominant-axis selection: +X,-X,+Y,-Y,+Z,-Z.
var cubeFaceDirs = [6]glm.Vec3f{{1, 0, 0}, {-1, 0, 0}, {0, 1, 0}, {0, -1, 0}, {0, 0, 1}, {0, 0, -1}}
var cubeFaceUps = [6]glm.Vec3f{{0, 1, 0}, {0, 1, 0}, {0, 0, 1}, {0, 0, -1}, {0, 1, 0}, {0, 1, 0}}
