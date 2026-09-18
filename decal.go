package pix

import (
	"github.com/bluescreen10/pix/geometries"
	"github.com/bluescreen10/pix/glm"
	"github.com/bluescreen10/pix/scenes"
)

// NewDecalGeometry builds a decal patch clipped to mesh and uploads it, returning the
// geometry to draw it with. The clipping itself is scene-side (see
// DecalGeometry); only the upload needs a renderer, which is why this is the one
// decal function that lives here.
//
//	geo := r.NewDecalGeometry(box, hit, orientation, glm.Vec3f{0.5, 0.5, 0.5})
//	decal := scene.NewMesh(geo, decalMat)
//	decal.SetCastShadow(false)
//	box.Add(decal)
//
// Returns the zero Geometry (Valid() reports false) when the box misses mesh entirely
// or every candidate triangle faces away — callers must check rather than assume a
// patch was produced.
func (r *Renderer) NewDecalGeometry(mesh scenes.Mesh, pos glm.Vec3f, orientation glm.Quatf, size glm.Vec3f) geometries.Geometry {
	cfg, ok := scenes.DecalGeometry(mesh, pos, orientation, size)
	if !ok {
		return geometries.Geometry{}
	}
	return r.GeometryStore.Create(cfg)
}
