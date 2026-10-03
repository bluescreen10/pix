// Light clusters: the main view cut into a grid of cells, each listing the point and
// spot lights that reach into it, so a pixel is shaded by its cell's lights rather than
// by every light in the scene (see light_clusters.comp.glsl and lightCluster in
// lighting.glsl).
package pix

import (
	"github.com/bluescreen10/gamekit/gpu"
	"github.com/bluescreen10/pix/glm"
	"github.com/bluescreen10/pix/scenes"
	"github.com/chewxy/math32"
)

// The cluster grid: clusterX x clusterY tiles across the screen, clusterZ slices in
// depth, spaced exponentially so a near slice is as thin, relative to its distance, as
// a far one. A cell lists up to clusterCapacity lights and drops the rest. They mirror
// CLUSTER_X, CLUSTER_Y, CLUSTER_Z and CLUSTER_CAPACITY in lighting.glsl.
const (
	clusterX        = 16
	clusterY        = 9
	clusterZ        = 24
	clusterCapacity = 255
)

// clusterCellsSize is the size of the grid's cells: each is a count followed by
// clusterCapacity light indices, all uint32.
const clusterCellsSize = clusterX * clusterY * clusterZ * (clusterCapacity + 1) * 4

// clusterBuildGroupSize is how many cells one workgroup of the build pass lists (see
// GROUP_SIZE in light_clusters.comp.glsl).
const clusterBuildGroupSize = 64

// clusterGrid is the main view's cluster grid for one frame: the cells the build pass
// fills, and what a world position needs to find its own cell — its tile through
// viewProj, its slice from its depth along depthPlane, as log(depth) * sliceScale +
// sliceBias. view and inverseProjection are what the build pass bounds the cells with.
type clusterGrid struct {
	cells             gpu.Buffer
	viewProj          glm.Mat4f
	depthPlane        glm.Vec4f
	sliceScale        float32
	sliceBias         float32
	view              glm.Mat4f
	inverseProjection glm.Mat4f
}

// lightClustersRoot matches PC in light_clusters.comp.glsl.
type lightClustersRoot struct {
	view              glm.Mat4f
	inverseProjection glm.Mat4f
	lights            uint64
	cells             uint64
}

// newClusterGrid lays a cluster grid over v, listing its lights into cells. The grid
// projects with the same Y flip as the main view draws with, so a cell's tile is the
// same whether a fragment finds it from its position or from where it lands on screen.
//
// The slices run from the view's near plane to its far plane, both read back out of its
// projection. An orthographic view may put its near plane at or behind the eye, where an
// exponential spacing has nowhere to start, so the first slice starts no nearer than a
// millionth of the far plane.
func newClusterGrid(v scenes.ViewPacket, cells gpu.Buffer) clusterGrid {
	projection := flipClipY(v.Projection)
	inverseProjection := projection.Inv()
	// Reversed depth: the near plane is at 1, the far plane at 0.
	near := viewDepthAt(inverseProjection, 1)
	far := viewDepthAt(inverseProjection, 0)
	near = max(near, far*1e-6)

	sliceScale := clusterZ / math32.Log(far/near)
	return clusterGrid{
		cells:             cells,
		viewProj:          projection.Mul4x4(v.View),
		depthPlane:        viewDepthPlane(v.View),
		sliceScale:        sliceScale,
		sliceBias:         -math32.Log(near) * sliceScale,
		view:              v.View,
		inverseProjection: inverseProjection,
	}
}

// viewDepthAt is the view depth of the plane at clip depth ndcDepth, for the
// projection whose inverse is inverseProjection.
func viewDepthAt(inverseProjection glm.Mat4f, ndcDepth float32) float32 {
	p := inverseProjection.Mul4x1(glm.Vec4f{0, 0, ndcDepth, 1})
	return -p[2] / p[3] // view space looks down -z
}
