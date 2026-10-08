package gltf_test

import (
	"encoding/base64"
	"encoding/binary"
	"encoding/json"
	"math"
	"os"
	"path/filepath"
	"testing"

	"github.com/bluescreen10/pix"
	"github.com/bluescreen10/pix/glm"
	"github.com/bluescreen10/pix/loaders/gltf"
	"github.com/bluescreen10/pix/scenes"
)

// morphAsset builds a glTF document with one node whose mesh has two identical
// triangle primitives and two morph targets:
//
//   - "up" moves every vertex up: its position deltas are quantized, as SHORT
//     components that are not normalized, so 3 means 3; its normal deltas are
//     normalized BYTEs, so 127 means 1.
//   - "right" is sparse with no buffer view: zeros, except vertex 2 moving 2 right.
//
// The mesh's default weights are [0.5 0]; nodeWeights, if given, overrides them. One
// animation drives the node's weights from [0 0] at t=0 to [1 1] at t=1.
func morphAsset(t *testing.T, nodeWeights []float32) string {
	t.Helper()
	var buf []byte
	align := func() {
		for len(buf)%4 != 0 {
			buf = append(buf, 0)
		}
	}
	type view struct{ offset, length, stride int }
	var views []view
	addView := func(stride int, write func()) int {
		align()
		start := len(buf)
		write()
		views = append(views, view{start, len(buf) - start, stride})
		return len(views) - 1
	}
	putFloats := func(values ...float32) {
		for _, v := range values {
			buf = binary.LittleEndian.AppendUint32(buf, math.Float32bits(v))
		}
	}

	positions := addView(0, func() { putFloats(-1, -1, 0, 1, -1, 0, 0, 1, 0) })
	normals := addView(0, func() { putFloats(0, 0, 1, 0, 0, 1, 0, 0, 1) })
	indices := addView(0, func() {
		for _, i := range []uint16{0, 1, 2} {
			buf = binary.LittleEndian.AppendUint16(buf, i)
		}
	})
	upPositions := addView(8, func() {
		for range 3 {
			for _, c := range []int16{0, 3, 0, 0} { // xyz plus padding to the 8-byte stride
				buf = binary.LittleEndian.AppendUint16(buf, uint16(c))
			}
		}
	})
	upNormals := addView(4, func() {
		for range 3 {
			buf = append(buf, 0, 127, 0, 0) // xyz plus padding to the 4-byte stride
		}
	})
	sparseIndices := addView(0, func() { buf = append(buf, 2) })
	sparseValues := addView(0, func() { putFloats(2, 0, 0) })
	times := addView(0, func() { putFloats(0, 1) })
	weights := addView(0, func() { putFloats(0, 0, 1, 1) })

	bufferViews := make([]map[string]any, len(views))
	for i, v := range views {
		bufferViews[i] = map[string]any{"buffer": 0, "byteOffset": v.offset, "byteLength": v.length}
		if v.stride != 0 {
			bufferViews[i]["byteStride"] = v.stride
		}
	}
	accessors := []map[string]any{
		{"bufferView": positions, "componentType": 5126, "count": 3, "type": "VEC3"},
		{"bufferView": normals, "componentType": 5126, "count": 3, "type": "VEC3"},
		{"bufferView": indices, "componentType": 5123, "count": 3, "type": "SCALAR"},
		{"bufferView": upPositions, "componentType": 5122, "count": 3, "type": "VEC3"},
		{"bufferView": upNormals, "componentType": 5120, "normalized": true, "count": 3, "type": "VEC3"},
		{"componentType": 5126, "count": 3, "type": "VEC3", "sparse": map[string]any{
			"count":   1,
			"indices": map[string]any{"bufferView": sparseIndices, "componentType": 5121},
			"values":  map[string]any{"bufferView": sparseValues},
		}},
		{"bufferView": times, "componentType": 5126, "count": 2, "type": "SCALAR"},
		{"bufferView": weights, "componentType": 5126, "count": 4, "type": "SCALAR"},
	}
	primitive := map[string]any{
		"attributes": map[string]int{"POSITION": 0, "NORMAL": 1},
		"indices":    2,
		"targets":    []map[string]int{{"POSITION": 3, "NORMAL": 4}, {"POSITION": 5}},
	}
	node := map[string]any{"mesh": 0, "name": "face"}
	if nodeWeights != nil {
		node["weights"] = nodeWeights
	}
	doc := map[string]any{
		"asset":  map[string]any{"version": "2.0"},
		"scene":  0,
		"scenes": []map[string]any{{"nodes": []int{0}}},
		"nodes":  []map[string]any{node},
		"meshes": []map[string]any{{
			"primitives": []map[string]any{primitive, primitive},
			"weights":    []float32{0.5, 0},
			"extras":     map[string]any{"targetNames": []string{"up", "right"}},
		}},
		"accessors":   accessors,
		"bufferViews": bufferViews,
		"buffers": []map[string]any{{
			"uri":        "data:application/octet-stream;base64," + base64.StdEncoding.EncodeToString(buf),
			"byteLength": len(buf),
		}},
		"animations": []map[string]any{{
			"channels": []map[string]any{{"sampler": 0, "target": map[string]any{"node": 0, "path": "weights"}}},
			"samplers": []map[string]any{{"input": 6, "output": 7, "interpolation": "LINEAR"}},
		}},
	}
	data, err := json.Marshal(doc)
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(t.TempDir(), "morph.gltf")
	if err := os.WriteFile(path, data, 0o644); err != nil {
		t.Fatal(err)
	}
	return path
}

// loadMorphAsset loads morphAsset and returns the meshes its weight animation drives.
func loadMorphAsset(t *testing.T, nodeWeights []float32) (*scenes.Scene, gltf.LoadResult, []scenes.Mesh) {
	t.Helper()
	r, err := pix.NewOffscreenRenderer(16, 16)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(r.Destroy)
	scene := scenes.New()
	t.Cleanup(scene.Destroy)
	result, err := gltf.Load(r, scene, morphAsset(t, nodeWeights), nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Clips) != 1 {
		t.Fatalf("loaded %d clips, want 1", len(result.Clips))
	}
	var meshes []scenes.Mesh
	for _, track := range result.Clips[0].Tracks {
		mesh, ok := track.Target.(scenes.Mesh)
		if !ok {
			t.Fatalf("weights track targets %T, want scenes.Mesh", track.Target)
		}
		meshes = append(meshes, mesh)
	}
	return scene, result, meshes
}

func TestLoadMorphTargets(t *testing.T) {
	_, result, meshes := loadMorphAsset(t, nil)
	if len(meshes) != 2 {
		t.Fatalf("the weights animation drives %d meshes, want 2 (one per primitive)", len(meshes))
	}
	geo := meshes[0].Geometry()

	if got := geo.MorphTargetCount(); got != 2 {
		t.Fatalf("MorphTargetCount() = %d, want 2", got)
	}
	if index, ok := geo.MorphTargetIndex("right"); !ok || index != 1 {
		t.Errorf(`MorphTargetIndex("right") = %d, %v, want 1, true`, index, ok)
	}

	t.Run("quantized deltas", func(t *testing.T) {
		up := geo.MorphTarget(0)
		for i, d := range up.PositionDeltas {
			if want := (glm.Vec3f{0, 3, 0}); d != want {
				t.Errorf("up.PositionDeltas[%d] = %v, want %v (SHORT 3, not normalized)", i, d, want)
			}
		}
		for i, d := range up.NormalDeltas {
			if want := (glm.Vec3f{0, 1, 0}); d != want {
				t.Errorf("up.NormalDeltas[%d] = %v, want %v (normalized BYTE 127)", i, d, want)
			}
		}
	})

	t.Run("sparse deltas", func(t *testing.T) {
		want := []glm.Vec3f{{0, 0, 0}, {0, 0, 0}, {2, 0, 0}}
		got := geo.MorphTarget(1).PositionDeltas
		for i := range want {
			if got[i] != want[i] {
				t.Errorf("right.PositionDeltas[%d] = %v, want %v", i, got[i], want[i])
			}
		}
	})

	t.Run("default weights", func(t *testing.T) {
		for i, mesh := range meshes {
			if got := mesh.MorphTargetWeights(); got[0] != 0.5 || got[1] != 0 {
				t.Errorf("mesh %d MorphTargetWeights() = %v, want the mesh's default [0.5 0]", i, got)
			}
		}
	})

	t.Run("weights track", func(t *testing.T) {
		for i, track := range result.Clips[0].Tracks {
			if track.Channel != scenes.ChannelMorphWeights {
				t.Errorf("track %d Channel = %v, want ChannelMorphWeights", i, track.Channel)
			}
			if len(track.Values) != 4 {
				t.Errorf("track %d has %d values, want 4 (2 keys of 2 weights)", i, len(track.Values))
			}
		}
	})
}

func TestLoadedUnnamedMeshTakesNodeName(t *testing.T) {
	scene, _, meshes := loadMorphAsset(t, nil)
	got, ok := scene.MeshByName("face")
	if !ok {
		t.Fatal(`MeshByName("face") found nothing, want the unnamed mesh of the node named "face"`)
	}
	if got.ID() != meshes[0].ID() && got.ID() != meshes[1].ID() {
		t.Errorf(`MeshByName("face") = %v, want one of the node's meshes`, got.ID())
	}
}

func TestLoadMorphTargetsNodeWeightsOverrideMesh(t *testing.T) {
	_, _, meshes := loadMorphAsset(t, []float32{0.25, 1})
	for i, mesh := range meshes {
		if got := mesh.MorphTargetWeights(); got[0] != 0.25 || got[1] != 1 {
			t.Errorf("mesh %d MorphTargetWeights() = %v, want the node's [0.25 1]", i, got)
		}
	}
}

func TestLoadedWeightsAnimationPlays(t *testing.T) {
	scene, result, meshes := loadMorphAsset(t, nil)
	mixer := scene.NewAnimationMixer(scene.Root())
	mixer.Action(result.Clips[0]).SetLoop(scenes.LoopOnce).Play()

	mixer.Update(0.5)
	for i, mesh := range meshes {
		if got := mesh.MorphTargetWeights(); got[0] != 0.5 || got[1] != 0.5 {
			t.Errorf("mesh %d MorphTargetWeights() at t=0.5 = %v, want [0.5 0.5]", i, got)
		}
	}
}
