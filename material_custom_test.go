package pix

import (
	"testing"
	"unsafe"

	"github.com/bluescreen10/pix/cameras"
	"github.com/bluescreen10/pix/colors"
	"github.com/bluescreen10/pix/glm"
	"github.com/bluescreen10/pix/internal/ref"
	"github.com/bluescreen10/pix/materials"
	"github.com/bluescreen10/pix/shaders"
)

// customMaterial is a material type defined OUTSIDE the materials package, built only
// from that package's exported API. It exists to prove the extensibility claim the
// package split was for: before it, Material's identity methods and Instance.Dispose
// were unexported, so no type outside the package could be a material at all.
//
// It reuses the basic shader (this is about the Go-side contract, not new shading) and
// serializes the same 48-byte record.
type customMaterial struct {
	pool *materials.Pool
	ref  ref.Ref

	color colors.RGBA32F
}

func newCustomMaterial(store *materials.Store) *customMaterial {
	p := store.Pool(materials.Shader{Forward: shaders.BasicForward}, "custom")
	m := &customMaterial{pool: p, color: colors.RGBA32F{0, 1, 0, 1}}
	m.ref = p.Create(m)
	return m
}

// --- materials.Instance ---

func (m *customMaterial) Bytes() []byte {
	rec := struct {
		color        colors.RGBA32F
		emissive     colors.RGB32F
		_            float32
		colorMap     uint32
		colorSampler uint32
		flags        uint32
		_            uint32
	}{color: m.color, colorMap: materials.NoTextureIndex}
	return unsafe.Slice((*byte)(unsafe.Pointer(&rec)), unsafe.Sizeof(rec))
}

func (m *customMaterial) Dispose() {} // holds no textures

// --- materials.Material ---

func (m *customMaterial) Copy() materials.Material { m.ref.Copy(); return m }
func (m *customMaterial) Release()                 { m.ref.Release() }
func (m *customMaterial) Valid() bool              { return m.ref.Valid() }

func (m *customMaterial) Vertex() []byte   { return m.pool.Shader().Vertex }
func (m *customMaterial) Forward() []byte  { return m.pool.Shader().Forward }
func (m *customMaterial) Deferred() []byte { return nil }
func (m *customMaterial) Lighting() []byte { return nil }

func (m *customMaterial) Cull() materials.CullMode   { return m.pool.Cull(m.ref.ID()) }
func (m *customMaterial) Blend() materials.BlendMode { return m.pool.Blend(m.ref.ID()) }

func (m *customMaterial) Hash() uint32        { return m.pool.Hash() }
func (m *customMaterial) ID() uint32          { return m.ref.ID() }
func (m *customMaterial) RecordsAddr() uint64 { return m.pool.RecordsAddr() }
func (m *customMaterial) SetColor(c colors.RGBA32F) {
	m.color = c
	m.pool.MarkDirty(m.ref.ID())
}

var _ materials.Material = (*customMaterial)(nil)

// TestCustomMaterialFromOutsideThePackage renders a mesh with a material type this
// package defines itself, all the way through the real pipeline: if any piece of the
// contract were still unexported, this would not compile, and if the renderer could
// not route a foreign Material, the cube would not come out green.
func TestCustomMaterialFromOutsideThePackage(t *testing.T) {
	r, err := NewOffscreenRenderer(64, 64)
	if err != nil {
		t.Fatal(err)
	}
	defer r.Destroy()

	scene := r.NewScene()
	cube := r.GeometryStore.Create(normalCube())
	defer cube.Release()

	mat := newCustomMaterial(r.MaterialStore)
	scene.Add(scene.NewMesh(cube, mat))

	cam := cameras.NewPerspectiveCamera(45, 1, 0.1, 100)
	cam.SetPosition(glm.Vec3f{0, 0, 2})

	red, green, blue := renderCube(t, r, scene, cam)
	if green < 200 || red > 80 || blue > 80 {
		t.Fatalf("custom material did not render green: r=%d g=%d b=%d", red, green, blue)
	}

	// And its setter must reach the GPU through the same dirty/Sync path as a built-in.
	mat.SetColor(colors.RGBA32F{1, 0, 0, 1})
	red, green, _ = renderCube(t, r, scene, cam)
	if red < 200 || green > 80 {
		t.Fatalf("custom material setter did not re-upload: r=%d g=%d", red, green)
	}
}
