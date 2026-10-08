package scenes

import (
	"fmt"

	"github.com/bluescreen10/pix/geometries"
	"github.com/bluescreen10/pix/materials"
)

// maxLODLevels is the most levels a single LOD group (Mesh, InstancedMesh or
// SkinnedMesh) can have — bounded by gpuLOD's fixed-size boundaries array.
const maxLODLevels = 4

// lodLevel is one entry in a LOD chain: geometry/material shown once the camera is
// farther than minDistance. Level 0's minDistance is always 0 (implicit — never
// set explicitly, the zero value is already correct) since it's the closest level;
// there is no separate "end" distance for a level — it runs until the next level's
// own minDistance takes over, and the last level has no upper bound at all.
//
// deformedGeometry is set on a coarser level of a morphed or skinned mesh whose
// geometry shares level 0's vertices: the level's triangles over the mesh's deform
// output, which is what it draws (see appendLODLevel). Zero on every other level.
type lodLevel struct {
	geometry         geometries.Geometry
	material         materials.Material
	minDistance      float32
	deformedGeometry geometries.Geometry
}

// drawnGeometry is the geometry the level is drawn with.
func (l *lodLevel) drawnGeometry() geometries.Geometry {
	if l.deformedGeometry.IsValid() {
		return l.deformedGeometry
	}
	return l.geometry
}

// appendLODLevel adds a coarser level to lods, taking its own references to geo and
// mat. deformOutput is the mesh's deform output, or the zero Geometry if the mesh is
// neither morphed nor skinned.
//
// On a deformed mesh, a level whose geometry shares level 0's vertices (see
// Geometry.CreateLOD) deforms with them: it is drawn as its triangles over the deform
// output. A level with vertices of its own is drawn as it is, which suits an impostor;
// one carrying morph targets or skin of its own would need a deformation of its own,
// which nothing provides, so it panics rather than draw undeformed.
func appendLODLevel(lods []lodLevel, geo geometries.Geometry, mat materials.Material, minDistance float32, deformOutput geometries.Geometry) []lodLevel {
	if len(lods) >= maxLODLevels {
		panic(fmt.Sprintf("pix: AddLOD: at most %d levels are supported", maxLODLevels))
	}
	if minDistance <= lods[len(lods)-1].minDistance {
		panic("pix: AddLOD levels must be added in increasing minDistance order")
	}
	level := lodLevel{geometry: geo.Copy(), material: mat.Copy(), minDistance: minDistance}
	if deformOutput.IsValid() {
		switch {
		case geo.SharesVerticesWith(lods[0].geometry):
			level.deformedGeometry = deformOutput.CreateLOD(geo.Indices())
		case geo.MorphTargetCount() > 0 || geo.IsSkinned():
			level.geometry.Release()
			level.material.Release()
			panic("pix: AddLOD on a morphed or skinned mesh needs a level made with CreateLOD from its geometry, or one without morph targets or skin")
		}
	}
	return append(lods, level)
}

// releaseLODs drops a LOD chain's references.
func releaseLODs(lods []lodLevel) {
	for _, l := range lods {
		l.geometry.Release()
		l.material.Release()
		l.deformedGeometry.Release()
	}
}
