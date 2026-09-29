package pix

import (
	"github.com/bluescreen10/pix/glm"
	"github.com/bluescreen10/pix/textures"
)

// frameViews is every camera a frame renders from: the main camera, and one per shadow
// map region.
type frameViews struct {
	main view
	// hasMainView says the packet had a view to render main from. Without one the frame
	// draws no scene, and has no shadow views either: they are fitted to the main one.
	hasMainView bool
	shadows     []view
	// eye is the main camera's position. Every view's cull uses it for LOD selection —
	// LOD is a main-camera decision whichever view is culling — and shading uses it for
	// specular.
	eye glm.Vec3f
}

// view is one camera the frame renders from, and the buffers its cull fills.
type view struct {
	// viewProj is what the vertex stage projects with, and planes what the cull tests
	// bounds against. For the main camera viewProj has clip-space Y flipped for drawing
	// (see flipClipY) and planes do not; a shadow camera uses neither flip.
	viewProj glm.Mat4f
	planes   [6]glm.Vec4f
	cull     *cullBuffers

	// Shadow views keep only shadow casters, and render into a region of a depth map:
	// a whole map for a spot light or a cube face, one square of the atlas for a
	// cascade. Views sharing a map must agree that exactly one of them clears it.
	castersOnly   bool
	shadowMap     textures.Texture
	x             int32
	width, height uint32
	clearsMap     bool
}
