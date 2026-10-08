package scenes

import (
	"fmt"

	"github.com/bluescreen10/pix/geometries"
	"github.com/bluescreen10/pix/glm"
	"github.com/chewxy/math32"
)

// morphState is a mesh's current blend toward each of its geometry's morph targets.
// revision changes with every weight change, so the renderer can tell a deform output
// that is still valid from one that has to be recomputed.
type morphState struct {
	weights  []float32
	revision uint64
}

func newMorphState(geo geometries.Geometry) morphState {
	return morphState{weights: make([]float32, geo.MorphTargetCount()), revision: 1}
}

// newMorphOutput allocates the geometry a morphing mesh draws, or returns the zero
// Geometry when geo has no morph targets to apply.
func newMorphOutput(geo geometries.Geometry) geometries.Geometry {
	if geo.MorphTargetCount() == 0 {
		return geometries.Geometry{}
	}
	return geo.CreateDeformOutput()
}

func (m *morphState) hasTargets() bool {
	return len(m.weights) > 0
}

func (m *morphState) setWeight(index int, weight float32) {
	if m.weights[index] == weight {
		return
	}
	m.weights[index] = weight
	m.revision++
}

func (m *morphState) setWeights(weights []float32) {
	if len(weights) != len(m.weights) {
		panic(fmt.Sprintf("pix: SetMorphTargetWeights got %d weights for %d morph targets", len(weights), len(m.weights)))
	}
	copy(m.weights, weights)
	m.revision++
}

// maxMorphDisplacement is the farthest any vertex of geo can be moved by its morph
// targets at these weights. A weight may be negative, which pushes the other way just
// as far.
func maxMorphDisplacement(geo geometries.Geometry, weights []float32) float32 {
	var displacement float32
	for i, w := range weights {
		if w != 0 {
			displacement += math32.Abs(w) * geo.MorphTargetMaxDisplacement(i)
		}
	}
	return displacement
}

// morphedBounds is geo's bounding sphere grown to hold every vertex at these weights.
func morphedBounds(geo geometries.Geometry, weights []float32) glm.Sphere {
	bounds := geo.BoundingSphere()
	bounds.Radius += maxMorphDisplacement(geo, weights)
	return bounds
}

// setMorphTargetWeights sets the morph target weights of whichever kind of mesh node
// is; any other kind of node has none, and is left alone.
func (s *Scene) setMorphTargetWeights(node NodeID, weights []float32) {
	s.validate(node)
	n := Node{scene: s, id: node}
	switch s.kind[node.index] {
	case kindMesh:
		Mesh{n}.SetMorphTargetWeights(weights)
	case kindInstancedMesh:
		InstancedMesh{n}.SetMorphTargetWeights(weights)
	case kindSkinnedMesh:
		SkinnedMesh{n}.SetMorphTargetWeights(weights)
	}
}

// MorphTargetCount returns how many morph targets the mesh's geometry has.
func (m Mesh) MorphTargetCount() int {
	return len(m.data().morph.weights)
}

// MorphTargetWeight returns the weight of the morph target at index.
func (m Mesh) MorphTargetWeight(index int) float32 {
	return m.data().morph.weights[index]
}

// MorphTargetWeights returns a copy of every morph target's weight, in target order.
func (m Mesh) MorphTargetWeights() []float32 {
	return append([]float32(nil), m.data().morph.weights...)
}

// SetMorphTargetWeight sets how far the mesh is blended toward the morph target at
// index: 0 leaves it alone, 1 applies it fully. Weights outside [0,1] extrapolate.
func (m Mesh) SetMorphTargetWeight(index int, weight float32) {
	md := m.data()
	md.morph.setWeight(index, weight)
	md.bounds = morphedBounds(md.lods[0].geometry, md.morph.weights)
}

// SetMorphTargetWeights sets every morph target's weight at once; weights must have
// one entry per target.
func (m Mesh) SetMorphTargetWeights(weights []float32) {
	md := m.data()
	md.morph.setWeights(weights)
	md.bounds = morphedBounds(md.lods[0].geometry, md.morph.weights)
}

// MorphTargetCount returns how many morph targets the field's geometry has.
func (m InstancedMesh) MorphTargetCount() int {
	return len(m.data().morph.weights)
}

// MorphTargetWeight returns the weight of the morph target at index.
func (m InstancedMesh) MorphTargetWeight(index int) float32 {
	return m.data().morph.weights[index]
}

// MorphTargetWeights returns a copy of every morph target's weight, in target order.
func (m InstancedMesh) MorphTargetWeights() []float32 {
	return append([]float32(nil), m.data().morph.weights...)
}

// SetMorphTargetWeight sets the weight of the morph target at index for every
// instance in the field — instances share one set of weights (see Mesh's method).
func (m InstancedMesh) SetMorphTargetWeight(index int, weight float32) {
	md := m.data()
	md.morph.setWeight(index, weight)
	md.bounds = morphedBounds(md.lods[0].geometry, md.morph.weights)
}

// SetMorphTargetWeights sets every morph target's weight at once, for every instance.
func (m InstancedMesh) SetMorphTargetWeights(weights []float32) {
	md := m.data()
	md.morph.setWeights(weights)
	md.bounds = morphedBounds(md.lods[0].geometry, md.morph.weights)
}

// MorphTargetCount returns how many morph targets the mesh's source geometry has.
func (m SkinnedMesh) MorphTargetCount() int {
	return len(m.data().morph.weights)
}

// MorphTargetWeight returns the weight of the morph target at index.
func (m SkinnedMesh) MorphTargetWeight(index int) float32 {
	return m.data().morph.weights[index]
}

// MorphTargetWeights returns a copy of every morph target's weight, in target order.
func (m SkinnedMesh) MorphTargetWeights() []float32 {
	return append([]float32(nil), m.data().morph.weights...)
}

// SetMorphTargetWeight sets the weight of the morph target at index. Morph targets
// apply to the bind pose, before skinning (see Mesh's method).
func (m SkinnedMesh) SetMorphTargetWeight(index int, weight float32) {
	m.data().morph.setWeight(index, weight)
}

// SetMorphTargetWeights sets every morph target's weight at once.
func (m SkinnedMesh) SetMorphTargetWeights(weights []float32) {
	m.data().morph.setWeights(weights)
}
