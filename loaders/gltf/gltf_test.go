package gltf

import (
	"encoding/base64"
	"encoding/binary"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"testing"

	"github.com/bluescreen10/pix"
	"github.com/bluescreen10/pix/glm"
)

// writeTriangleGLTF writes a minimal .gltf: one triangle mesh under a parent node
// that translates it +0.5 in x, with a green PBR base color. Returns the path.
func writeTriangleGLTF(t *testing.T) string {
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
      "meshes": [{"primitives": [{"attributes": {"POSITION": 0}, "indices": 1, "material": 0}]}],
      "materials": [{"pbrMetallicRoughness": {"baseColorFactor": [0.2, 0.85, 0.3, 1]}}],
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
	scene := r.NewScene()
	defer scene.Destroy()

	n, err := Load(r, scene, writeTriangleGLTF(t))
	if err != nil {
		t.Fatal(err)
	}
	if n != 1 || scene.MeshCount() != 1 {
		t.Fatalf("loaded %d meshes (scene has %d), want 1", n, scene.MeshCount())
	}

	cam := pix.NewCamera()
	cam.Position = glm.Vec3f{0, 0, 2.5}
	cam.FOV = 50
	cam.Far = 100
	r.Render(scene, cam)

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
