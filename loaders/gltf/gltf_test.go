package gltf_test

import (
	"bytes"
	"encoding/base64"
	"encoding/binary"
	"fmt"
	"image"
	"image/png"
	"math"
	"os"
	"path/filepath"
	"testing"

	"github.com/bluescreen10/pix"
	"github.com/bluescreen10/pix/colors"
	"github.com/bluescreen10/pix/glm"
	"github.com/bluescreen10/pix/loaders/gltf"
	"github.com/bluescreen10/pix/materials"
	"github.com/bluescreen10/pix/scenes"
)

// TestMultiSceneOnlyDefault verifies the loader builds ONLY the default scene's
// nodes. A file with two scenes (each with its own mesh node) must load just the
// default scene's mesh — the other scene's node must not be created (it would
// otherwise render at identity/origin).
func TestMultiSceneOnlyDefault(t *testing.T) {
	// Two triangle meshes; scene 0 uses node 0, scene 1 uses node 1.
	var buf []byte
	putf := func(f float32) { buf = binary.LittleEndian.AppendUint32(buf, math.Float32bits(f)) }
	for _, p := range [][3]float32{{-0.6, -0.6, 0}, {0.6, -0.6, 0}, {0, 0.6, 0}} {
		putf(p[0])
		putf(p[1])
		putf(p[2])
	}
	posLen := len(buf)
	for _, i := range []uint16{0, 1, 2} {
		buf = binary.LittleEndian.AppendUint16(buf, i)
	}
	idxLen := len(buf) - posLen
	uri := "data:application/octet-stream;base64," + base64.StdEncoding.EncodeToString(buf)
	doc := fmt.Sprintf(`{
      "asset": {"version": "2.0"},
      "scene": 0,
      "scenes": [{"name":"A","nodes": [0]}, {"name":"B","nodes": [1]}],
      "nodes": [
        {"mesh": 0, "translation": [0, 0, 0]},
        {"mesh": 0, "translation": [100, 0, 0]}
      ],
      "meshes": [{"primitives": [{"attributes": {"POSITION": 0}, "indices": 1}]}],
      "accessors": [
        {"bufferView": 0, "componentType": 5126, "count": 3, "type": "VEC3"},
        {"bufferView": 1, "componentType": 5123, "count": 3, "type": "SCALAR"}
      ],
      "bufferViews": [
        {"buffer": 0, "byteOffset": 0, "byteLength": %d},
        {"buffer": 0, "byteOffset": %d, "byteLength": %d}
      ],
      "buffers": [{"uri": "%s", "byteLength": %d}]
    }`, posLen, posLen, idxLen, uri, len(buf))
	path := filepath.Join(t.TempDir(), "two.gltf")
	if err := os.WriteFile(path, []byte(doc), 0o644); err != nil {
		t.Fatal(err)
	}

	r, err := pix.NewOffscreenRenderer(32, 32)
	if err != nil {
		t.Fatal(err)
	}
	defer r.Destroy()
	scene := scenes.New()
	defer scene.Destroy()
	res, err := gltf.Load(r, scene, path, nil)
	if err != nil {
		t.Fatal(err)
	}
	if res.Added != 1 || scene.MeshCount() != 1 {
		t.Fatalf("loaded %d meshes (scene has %d), want 1 — the second scene's node leaked", res.Added, scene.MeshCount())
	}
}

// writeTriangleGLTF writes a minimal .gltf: one triangle mesh under a parent node
// that translates it +0.5 in x, with a green PBR base color. Returns the path.
func writeTriangleGLTF(t *testing.T) string {
	t.Helper()
	return writeTriangleGLTFWithMaterial(t, `{"pbrMetallicRoughness": {"baseColorFactor": [0.2, 0.85, 0.3, 1]}}`, "")
}

// writeTriangleGLTFWithMaterial writes writeTriangleGLTF's triangle, a mesh named
// "triangle", with the material material (glTF JSON), and extra top-level members —
// the images and textures the material uses, each followed by a comma — beside the
// document's own. Returns the path.
func writeTriangleGLTFWithMaterial(t *testing.T, material, extra string) string {
	t.Helper()
	var buf []byte
	putf := func(f float32) { buf = binary.LittleEndian.AppendUint32(buf, math.Float32bits(f)) }
	// 3 positions (vec3), a triangle in the z=0 plane.
	for _, p := range [][3]float32{{-0.6, -0.6, 0}, {0.6, -0.6, 0}, {0, 0.6, 0}} {
		putf(p[0])
		putf(p[1])
		putf(p[2])
	}
	posLen := len(buf) // 36
	// 3 indices (ushort).
	for _, i := range []uint16{0, 1, 2} {
		buf = binary.LittleEndian.AppendUint16(buf, i)
	}
	idxLen := len(buf) - posLen // 6

	uri := "data:application/octet-stream;base64," + base64.StdEncoding.EncodeToString(buf)
	doc := fmt.Sprintf(`{
      "asset": {"version": "2.0"},
      "scene": 0,
      "scenes": [{"nodes": [0]}],
      "nodes": [
        {"children": [1], "translation": [0.5, 0, 0]},
        {"mesh": 0}
      ],
      "meshes": [{"name": "triangle", "primitives": [{"attributes": {"POSITION": 0}, "indices": 1, "material": 0}]}],
      "materials": [%s],
      %s
      "accessors": [
        {"bufferView": 0, "componentType": 5126, "count": 3, "type": "VEC3"},
        {"bufferView": 1, "componentType": 5123, "count": 3, "type": "SCALAR"}
      ],
      "bufferViews": [
        {"buffer": 0, "byteOffset": 0, "byteLength": %d},
        {"buffer": 0, "byteOffset": %d, "byteLength": %d}
      ],
      "buffers": [{"uri": "%s", "byteLength": %d}]
    }`, material, extra, posLen, posLen, idxLen, uri, len(buf))

	path := filepath.Join(t.TempDir(), "tri.gltf")
	if err := os.WriteFile(path, []byte(doc), 0o644); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestLoadTriangle(t *testing.T) {
	const size = 160
	r, err := pix.NewOffscreenRenderer(size, size)
	if err != nil {
		t.Fatal(err)
	}
	defer r.Destroy()
	scene := scenes.New()
	defer scene.Destroy()

	res, err := gltf.Load(r, scene, writeTriangleGLTF(t), nil)
	if err != nil {
		t.Fatal(err)
	}
	if res.Added != 1 || scene.MeshCount() != 1 {
		t.Fatalf("loaded %d meshes (scene has %d), want 1", res.Added, scene.MeshCount())
	}

	// Flat white ambient so the unlit base color shows through (this test checks
	// loading/material/transform, not lighting — and there's no default ambient).
	scene.SetAmbient(colors.RGB32F{1, 1, 1}, 1)

	cam := scene.NewPerspectiveCamera(50, 1, 0.1, 100)
	scene.Add(cam)
	cam.SetPosition(glm.Vec3f{0, 0, 2.5})
	r.Render(scene)

	px := r.Pixels()
	var lit, green, leftLit, rightLit int
	for y := 0; y < size; y++ {
		for x := 0; x < size; x++ {
			i := (y*size + x) * 4
			r, g, bl := px[i], px[i+1], px[i+2]
			if r == 0 && g == 0 && bl == 0 {
				continue
			}
			lit++
			if g > r && g > bl {
				green++
			}
			if x < size/2 {
				leftLit++
			} else {
				rightLit++
			}
		}
	}
	t.Logf("lit=%d green=%d left=%d right=%d", lit, green, leftLit, rightLit)
	if lit < 500 {
		t.Fatalf("triangle barely rendered (%d px)", lit)
	}
	if green < lit/2 {
		t.Fatalf("material base color not applied (green=%d of %d lit)", green, lit)
	}
	// The parent node translates +x, so the triangle sits right of center.
	if rightLit <= leftLit {
		t.Fatalf("node-hierarchy transform not applied: left=%d right=%d", leftLit, rightLit)
	}
}

// loadTriangleMaterial loads writeTriangleGLTFWithMaterial's triangle with material
// (glTF JSON) and extra top-level members, and returns the PBR material it was given.
func loadTriangleMaterial(t *testing.T, material, extra string) *materials.PBRMaterial {
	t.Helper()
	r, err := pix.NewOffscreenRenderer(8, 8)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(r.Destroy)
	scene := scenes.New()
	t.Cleanup(scene.Destroy)
	if _, err := gltf.Load(r, scene, writeTriangleGLTFWithMaterial(t, material, extra), nil); err != nil {
		t.Fatal(err)
	}
	node, ok := scene.FindByName("triangle")
	if !ok {
		t.Fatal("no mesh named triangle")
	}
	pbr, ok := scenes.Mesh{Node: node}.Material().(*materials.PBRMaterial)
	if !ok {
		t.Fatalf("triangle's material is %T, want *materials.PBRMaterial", scenes.Mesh{Node: node}.Material())
	}
	return pbr
}

// TestLoadGlassVolume: KHR_materials_ior and KHR_materials_volume give a glass material
// its index of refraction, thickness and absorption.
func TestLoadGlassVolume(t *testing.T) {
	glass := loadTriangleMaterial(t, `{
      "extensions": {
        "KHR_materials_transmission": {"transmissionFactor": 1},
        "KHR_materials_ior": {"ior": 1.33},
        "KHR_materials_volume": {"thicknessFactor": 0.2, "attenuationDistance": 3, "attenuationColor": [0.8, 0.9, 1]}
      }
    }`, "")
	if got := glass.IOR(); got != 1.33 {
		t.Errorf("IOR() = %v, want 1.33", got)
	}
	if got := glass.Thickness(); got != 0.2 {
		t.Errorf("Thickness() = %v, want 0.2", got)
	}
	if got := glass.AttenuationDistance(); got != 3 {
		t.Errorf("AttenuationDistance() = %v, want 3", got)
	}
	if got := glass.AttenuationColor(); got != (colors.RGB32F{0.8, 0.9, 1}) {
		t.Errorf("AttenuationColor() = %v, want {0.8 0.9 1}", got)
	}
}

// TestLoadGlassVolumeDefaults: without the extensions, glass is a thin wall of index
// 1.5 that absorbs nothing, as glTF defines them.
func TestLoadGlassVolumeDefaults(t *testing.T) {
	glass := loadTriangleMaterial(t, `{"extensions": {"KHR_materials_transmission": {"transmissionFactor": 1}}}`, "")
	if glass.IOR() != 1.5 || glass.Thickness() != 0 || glass.AttenuationDistance() != 0 {
		t.Errorf("IOR, Thickness, AttenuationDistance = %v, %v, %v, want 1.5, 0, 0", glass.IOR(), glass.Thickness(), glass.AttenuationDistance())
	}
}

// TestLoadAlphaMode: only alphaMode BLEND takes a material's alpha as how much of its
// surface is there; OPAQUE, the default, ignores it, and so does MASK, once its cut-off
// has decided where the surface is.
func TestLoadAlphaMode(t *testing.T) {
	for _, tc := range []struct {
		material string
		ignores  bool
	}{
		{`{}`, true},
		{`{"alphaMode": "OPAQUE"}`, true},
		{`{"alphaMode": "MASK"}`, true},
		{`{"alphaMode": "BLEND"}`, false},
	} {
		t.Run(tc.material, func(t *testing.T) {
			if got := loadTriangleMaterial(t, tc.material, "").IgnoresAlpha(); got != tc.ignores {
				t.Errorf("IgnoresAlpha() = %v, want %v", got, tc.ignores)
			}
		})
	}
}

// TestLoadEmissive: a material's emissive factor, scaled by KHR_materials_emissive_
// strength, becomes its emissive colour, and its emissive texture its emissive map.
func TestLoadEmissive(t *testing.T) {
	var encoded bytes.Buffer
	pixel := image.NewNRGBA(image.Rect(0, 0, 1, 1))
	copy(pixel.Pix, []byte{255, 128, 0, 255})
	if err := png.Encode(&encoded, pixel); err != nil {
		t.Fatal(err)
	}
	images := fmt.Sprintf(`"images": [{"uri": "data:image/png;base64,%s"}], "textures": [{"source": 0}],`, base64.StdEncoding.EncodeToString(encoded.Bytes()))

	lamp := loadTriangleMaterial(t, `{
      "emissiveFactor": [1, 0.5, 0.25],
      "emissiveTexture": {"index": 0},
      "extensions": {"KHR_materials_emissive_strength": {"emissiveStrength": 4}}
    }`, images)
	if got := lamp.Emissive(); got != (colors.RGB32F{4, 2, 1}) {
		t.Errorf("Emissive() = %v, want {4 2 1}: the factor times the strength", got)
	}
	if !lamp.EmissiveMap().IsValid() {
		t.Errorf("EmissiveMap() is not set, want the emissive texture")
	}
}

// TestLoadEmissiveDefault: a material that says nothing of emission emits nothing.
func TestLoadEmissiveDefault(t *testing.T) {
	dark := loadTriangleMaterial(t, `{}`, "")
	if got := dark.Emissive(); got != (colors.RGB32F{}) {
		t.Errorf("Emissive() of a material with none = %v, want black", got)
	}
	if dark.EmissiveMap().IsValid() {
		t.Errorf("EmissiveMap() is set, want none")
	}
}
