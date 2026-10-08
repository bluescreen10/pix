package scenes_test

import (
	"testing"

	gputest "github.com/bluescreen10/gamekit/gpu/test"
	"github.com/bluescreen10/pix/geometries"
	"github.com/bluescreen10/pix/glm"
	"github.com/bluescreen10/pix/scenes"
)

func newGeometryStore(t *testing.T) *geometries.Store {
	t.Helper()
	backend := gputest.New()
	if err := backend.Init(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(backend.Destroy)
	store := geometries.NewStore(backend)
	t.Cleanup(store.Destroy)
	return store
}

// morphedTriangle is a triangle with two targets: "lift" moves every vertex 2 up,
// "widen" moves the right corner 1 to the right.
func morphedTriangle(t *testing.T, store *geometries.Store) geometries.Geometry {
	t.Helper()
	positions := []glm.Vec3f{{-1, -1, 0}, {1, -1, 0}, {0, 1, 0}}
	geo := store.Create(geometries.GeometryConfig{
		Attributes: []geometries.Attribute{
			geometries.NewAttribute(geometries.AttributePosition, geometries.Float32x3, positions),
		},
		MorphTargets: []geometries.MorphTarget{
			{Name: "lift", PositionDeltas: []glm.Vec3f{{0, 2, 0}, {0, 2, 0}, {0, 2, 0}}},
			{Name: "widen", PositionDeltas: []glm.Vec3f{{0, 0, 0}, {1, 0, 0}, {0, 0, 0}}},
		},
	})
	t.Cleanup(geo.Release)
	return geo
}

func plainTriangle(t *testing.T, store *geometries.Store) geometries.Geometry {
	t.Helper()
	positions := []glm.Vec3f{{-1, -1, 0}, {1, -1, 0}, {0, 1, 0}}
	geo := store.Create(geometries.GeometryConfig{
		Attributes: []geometries.Attribute{
			geometries.NewAttribute(geometries.AttributePosition, geometries.Float32x3, positions),
		},
	})
	t.Cleanup(geo.Release)
	return geo
}

// skinnedTriangle is a triangle fully weighted to joint 0.
func skinnedTriangle(t *testing.T, store *geometries.Store) geometries.Geometry {
	t.Helper()
	positions := []glm.Vec3f{{-1, -1, 0}, {1, -1, 0}, {0, 1, 0}}
	joints := []glm.Vec4[uint16]{{0, 0, 0, 0}, {0, 0, 0, 0}, {0, 0, 0, 0}}
	weights := []glm.Vec4f{{1, 0, 0, 0}, {1, 0, 0, 0}, {1, 0, 0, 0}}
	geo := store.Create(geometries.GeometryConfig{
		Attributes: []geometries.Attribute{
			geometries.NewAttribute(geometries.AttributePosition, geometries.Float32x3, positions),
			geometries.NewAttribute(geometries.AttributeSkinIndex, geometries.Uint16x4, joints),
			geometries.NewAttribute(geometries.AttributeSkinWeight, geometries.Float32x4, weights),
		},
	})
	t.Cleanup(geo.Release)
	return geo
}

func TestMeshMorphTargetWeights(t *testing.T) {
	store := newGeometryStore(t)
	scene := scenes.New()
	defer scene.Destroy()
	mesh := scene.NewMesh(morphedTriangle(t, store), newFakeMaterial())

	if got := mesh.MorphTargetCount(); got != 2 {
		t.Fatalf("MorphTargetCount() = %d, want 2", got)
	}
	if got := mesh.MorphTargetWeight(1); got != 0 {
		t.Errorf("initial MorphTargetWeight(1) = %v, want 0", got)
	}

	mesh.SetMorphTargetWeight(1, 0.25)
	if got := mesh.MorphTargetWeight(1); got != 0.25 {
		t.Errorf("MorphTargetWeight(1) = %v after setting 0.25", got)
	}

	mesh.SetMorphTargetWeights([]float32{0.5, 0.75})
	weights := mesh.MorphTargetWeights()
	if weights[0] != 0.5 || weights[1] != 0.75 {
		t.Errorf("MorphTargetWeights() = %v, want [0.5 0.75]", weights)
	}

	weights[0] = 9
	if got := mesh.MorphTargetWeight(0); got != 0.5 {
		t.Errorf("MorphTargetWeight(0) = %v after changing the returned slice, want 0.5 (a copy)", got)
	}
}

func TestMeshSetMorphTargetWeightsCountMismatchPanics(t *testing.T) {
	store := newGeometryStore(t)
	scene := scenes.New()
	defer scene.Destroy()
	mesh := scene.NewMesh(morphedTriangle(t, store), newFakeMaterial())

	defer func() {
		if recover() == nil {
			t.Errorf("SetMorphTargetWeights with 1 weight for 2 targets did not panic")
		}
	}()
	mesh.SetMorphTargetWeights([]float32{1})
}

func TestMeshMorphBoundsFollowWeights(t *testing.T) {
	store := newGeometryStore(t)
	scene := scenes.New()
	defer scene.Destroy()
	geo := morphedTriangle(t, store)
	mesh := scene.NewMesh(geo, newFakeMaterial())
	base := geo.BoundingSphere()

	tests := []struct {
		name    string
		weights []float32
		grow    float32
	}{
		{"rest", []float32{0, 0}, 0},
		{"half lift", []float32{0.5, 0}, 1},
		{"negative lift", []float32{-1, 0}, 2},
		{"both", []float32{1, 1}, 3},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mesh.SetMorphTargetWeights(tt.weights)
			got := mesh.BoundingSphere()
			want := glm.Sphere{Center: base.Center, Radius: base.Radius + tt.grow}
			if got != want {
				t.Errorf("BoundingSphere() at weights %v = %v, want %v", tt.weights, got, want)
			}
		})
	}
}

func TestMeshAddLODWithMorphTargetsPanics(t *testing.T) {
	store := newGeometryStore(t)
	scene := scenes.New()
	defer scene.Destroy()
	mesh := scene.NewMesh(morphedTriangle(t, store), newFakeMaterial())

	defer func() {
		if recover() == nil {
			t.Errorf("AddLOD on a mesh with morph targets did not panic")
		}
	}()
	mesh.AddLOD(plainTriangle(t, store), newFakeMaterial(), 10)
}

func TestExtractMorphedMesh(t *testing.T) {
	store := newGeometryStore(t)
	scene := scenes.New()
	defer scene.Destroy()
	geo := morphedTriangle(t, store)
	mesh := scene.NewMesh(geo, newFakeMaterial())
	scene.Add(mesh)
	scene.Add(scene.NewMesh(plainTriangle(t, store), newFakeMaterial()))

	var packet scenes.FramePacket
	scene.Extract(&packet)

	if len(packet.Deforms.Data) != 1 {
		t.Fatalf("len(Deforms) = %d, want 1 (only the morphed mesh)", len(packet.Deforms.Data))
	}
	deform := packet.Deforms.Data[0]
	if deform.Source != geo.ID() {
		t.Errorf("Deforms[0].Source = %v, want the mesh's geometry %v", deform.Source, geo.ID())
	}
	if deform.Output == geo.ID() {
		t.Errorf("Deforms[0].Output is the source geometry, want a separate output")
	}
	if drawn := packet.Meshes.Data[deform.Mesh].Geometry; drawn != deform.Output {
		t.Errorf("mesh draws geometry %v, want the deform output %v", drawn, deform.Output)
	}
	if deform.VertexCount != 3 {
		t.Errorf("Deforms[0].VertexCount = %d, want 3", deform.VertexCount)
	}
	if deform.Joints.Count != 0 {
		t.Errorf("Deforms[0].Joints.Count = %d, want 0 for an unskinned mesh", deform.Joints.Count)
	}
	if deform.MorphWeights.Count != 0 {
		t.Errorf("Deforms[0].MorphWeights.Count = %d at rest, want 0", deform.MorphWeights.Count)
	}

	t.Run("only nonzero weights", func(t *testing.T) {
		mesh.SetMorphTargetWeights([]float32{0, 0.5})
		scene.Extract(&packet)
		weights := packet.Deforms.Data[0].MorphWeights
		got := packet.MorphWeights.Data[weights.First : weights.First+weights.Count]
		want := []scenes.MorphWeight{{Target: 1, Weight: 0.5}}
		if len(got) != len(want) || got[0] != want[0] {
			t.Errorf("published morph weights = %v, want %v", got, want)
		}
	})

	t.Run("revision follows weights", func(t *testing.T) {
		scene.Extract(&packet)
		before := packet.Deforms.Data[0].MorphRevision
		scene.Extract(&packet)
		if got := packet.Deforms.Data[0].MorphRevision; got != before {
			t.Errorf("MorphRevision = %d with no weight change, want %d", got, before)
		}
		mesh.SetMorphTargetWeight(0, 1)
		scene.Extract(&packet)
		if got := packet.Deforms.Data[0].MorphRevision; got == before {
			t.Errorf("MorphRevision = %d after a weight change, want it changed", got)
		}
	})

	t.Run("bounds follow weights without a mesh table rebuild", func(t *testing.T) {
		mesh.SetMorphTargetWeights([]float32{0, 0})
		scene.Extract(&packet)
		revision := packet.Meshes.Revision

		mesh.SetMorphTargetWeights([]float32{1, 0})
		scene.Extract(&packet)
		if packet.Meshes.Revision != revision {
			t.Errorf("Meshes.Revision = %d after a weight change, want %d (no rebuild)", packet.Meshes.Revision, revision)
		}
		got := packet.Meshes.Data[packet.Deforms.Data[0].Mesh].Bounds
		if want := mesh.BoundingSphere(); got != want {
			t.Errorf("published bounds = %v, want %v", got, want)
		}
	})
}

func TestExtractSkipsUnattachedMorphedMesh(t *testing.T) {
	store := newGeometryStore(t)
	scene := scenes.New()
	defer scene.Destroy()
	scene.NewMesh(morphedTriangle(t, store), newFakeMaterial())

	var packet scenes.FramePacket
	scene.Extract(&packet)
	if len(packet.Deforms.Data) != 0 {
		t.Errorf("len(Deforms) = %d for a mesh never added to the scene, want 0", len(packet.Deforms.Data))
	}
}

func TestInstancedMeshSharesMorphWeights(t *testing.T) {
	store := newGeometryStore(t)
	scene := scenes.New()
	defer scene.Destroy()
	transforms := []glm.Mat4f{glm.Mat4fIdentity, glm.Mat4fIdentity, glm.Mat4fIdentity}
	field := scene.NewInstancedMesh(morphedTriangle(t, store), newFakeMaterial(), transforms)
	scene.Add(field)
	field.SetMorphTargetWeight(0, 1)

	var packet scenes.FramePacket
	scene.Extract(&packet)
	if len(packet.Deforms.Data) != 1 {
		t.Fatalf("len(Deforms) = %d for one instanced mesh, want 1 shared by every instance", len(packet.Deforms.Data))
	}
	mesh := packet.Meshes.Data[packet.Deforms.Data[0].Mesh]
	if mesh.Transforms.Count != 3 {
		t.Errorf("deformed mesh draws %d instances, want 3", mesh.Transforms.Count)
	}
}

func TestSkinnedMeshMorphBoundsFollowWeights(t *testing.T) {
	store := newGeometryStore(t)
	scene := scenes.New()
	defer scene.Destroy()

	positions := []glm.Vec3f{{-1, 0, 0}, {1, 0, 0}, {0, 2, 0}}
	joints := []glm.Vec4[uint16]{{0, 0, 0, 0}, {0, 0, 0, 0}, {0, 0, 0, 0}}
	skinWeights := []glm.Vec4f{{1, 0, 0, 0}, {1, 0, 0, 0}, {1, 0, 0, 0}}
	geo := store.Create(geometries.GeometryConfig{
		Attributes: []geometries.Attribute{
			geometries.NewAttribute(geometries.AttributePosition, geometries.Float32x3, positions),
			geometries.NewAttribute(geometries.AttributeSkinIndex, geometries.Uint16x4, joints),
			geometries.NewAttribute(geometries.AttributeSkinWeight, geometries.Float32x4, skinWeights),
		},
		MorphTargets: []geometries.MorphTarget{
			{Name: "stretch", PositionDeltas: []glm.Vec3f{{0, 0, 0}, {0, 0, 0}, {0, 3, 0}}},
		},
	})
	defer geo.Release()

	skeleton := scene.NewSkeleton(scenes.SkeletonConfig{
		Parents:     []int32{-1},
		InverseBind: []glm.Mat4f{glm.Mat4fIdentity},
		BindPose:    []scenes.Transform{{Rotation: glm.QuatfIdentity, Scale: glm.Vec3f{1, 1, 1}}},
	})
	scene.Add(skeleton)
	mesh := scene.NewSkinnedMesh(geo, newFakeMaterial(), skeleton)
	scene.Add(mesh)

	scene.Sync()
	rest := mesh.BoundingSphere()
	mesh.SetMorphTargetWeight(0, 1)
	scene.Sync()
	stretched := mesh.BoundingSphere()

	if want := rest.Radius + 3; stretched.Radius != want {
		t.Errorf("BoundingSphere().Radius = %v at weight 1, want %v (rest %v plus the target's 3)", stretched.Radius, want, rest.Radius)
	}
}

func TestAnimationMixerDrivesMorphWeights(t *testing.T) {
	store := newGeometryStore(t)
	scene := scenes.New()
	defer scene.Destroy()
	mesh := scene.NewMesh(morphedTriangle(t, store), newFakeMaterial())
	scene.Add(mesh)

	clip := &scenes.AnimationClip{
		Name:     "lift",
		Duration: 2,
		Tracks: []scenes.Track{{
			Target:  mesh,
			Channel: scenes.ChannelMorphWeights,
			Interp:  scenes.InterpLinear,
			Times:   []float32{0, 2},
			Values:  []float32{0, 1, 1, 0},
		}},
	}
	mixer := scene.NewAnimationMixer(mesh)
	mixer.Action(clip).SetLoop(scenes.LoopOnce).Play()

	mixer.Update(0.5)
	if got := mesh.MorphTargetWeights(); got[0] != 0.25 || got[1] != 0.75 {
		t.Errorf("MorphTargetWeights() at t=0.5 = %v, want [0.25 0.75]", got)
	}
}

func TestAnimationMixerBlendsMorphWeights(t *testing.T) {
	store := newGeometryStore(t)
	scene := scenes.New()
	defer scene.Destroy()
	mesh := scene.NewMesh(morphedTriangle(t, store), newFakeMaterial())
	scene.Add(mesh)

	pose := func(name string, weights ...float32) *scenes.AnimationClip {
		return &scenes.AnimationClip{
			Name:     name,
			Duration: 1,
			Tracks: []scenes.Track{{
				Target: mesh, Channel: scenes.ChannelMorphWeights, Interp: scenes.InterpStep,
				Times: []float32{0}, Values: weights,
			}},
		}
	}
	mixer := scene.NewAnimationMixer(mesh)
	mixer.Action(pose("lifted", 1, 0)).SetWeight(3).Play()
	mixer.Action(pose("widened", 0, 1)).SetWeight(1).Play()

	mixer.Update(0)
	if got := mesh.MorphTargetWeights(); got[0] != 0.75 || got[1] != 0.25 {
		t.Errorf("MorphTargetWeights() blending 3:1 = %v, want [0.75 0.25]", got)
	}
}
