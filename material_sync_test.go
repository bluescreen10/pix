package pix

import (
	"testing"

	"github.com/bluescreen10/pix/cameras"
	"github.com/bluescreen10/pix/colors"
	"github.com/bluescreen10/pix/glm"
	"github.com/bluescreen10/pix/textures"
)

// renderCube draws one unlit cube filling the view and returns the centre pixel.
func renderCube(t *testing.T, r *Renderer, scene *Scene, cam Camera) (byte, byte, byte) {
	t.Helper()
	r.Render(scene, cam)
	px := r.Pixels()
	w, _ := r.Size()
	i := (int(w)/2*int(w) + int(w)/2) * 4
	return px[i], px[i+1], px[i+2]
}

// TestMaterialEditBetweenFramesReachesGPU is the core guard for device-local material
// records. Records live in MemoryDevice and are written only by materialStore.Sync, so
// an accessor that forgets to mark its record dirty updates the host shadow and the
// GPU never sees it — the frame would silently keep rendering the old value.
func TestMaterialEditBetweenFramesReachesGPU(t *testing.T) {
	r, err := NewOffscreenRenderer(64, 64)
	if err != nil {
		t.Fatal(err)
	}
	defer r.Destroy()

	scene := r.NewScene()
	cube := r.GeometryStore.Create(normalCube())
	defer cube.Release()
	mat := r.NewBasicMaterial() // unlit: pixel is the material color, no lighting
	mat.SetColor(colors.RGBA32F{1, 0, 0, 1})
	scene.Add(scene.NewMesh(cube, mat))

	cam := cameras.NewPerspectiveCamera(45, 1, 0.1, 100)
	cam.SetPosition(glm.Vec3f{0, 0, 2})

	red, _, _ := renderCube(t, r, scene, cam)
	if red < 200 {
		t.Fatalf("first frame: want a red cube, got r=%d", red)
	}

	// Change the material and re-render. Nothing else about the scene changes.
	mat.SetColor(colors.RGBA32F{0, 0, 1, 1})
	r2, _, b2 := renderCube(t, r, scene, cam)
	if b2 < 200 || r2 > 60 {
		t.Fatalf("material edit never reached the GPU: want blue, got r=%d b=%d", r2, b2)
	}
}

// TestMaterialStoreGrowReuploadsEveryRecord covers the other half: growing reallocates
// the device buffer, whose contents are undefined, so every live record must be
// re-uploaded and not just the ones edited since the last Sync.
func TestMaterialStoreGrowReuploadsEveryRecord(t *testing.T) {
	r, err := NewOffscreenRenderer(64, 64)
	if err != nil {
		t.Fatal(err)
	}
	defer r.Destroy()

	scene := r.NewScene()
	cube := r.GeometryStore.Create(normalCube())
	defer cube.Release()

	first := r.NewBasicMaterial()
	first.SetColor(colors.RGBA32F{1, 0, 0, 1})
	scene.Add(scene.NewMesh(cube, first))

	cam := cameras.NewPerspectiveCamera(45, 1, 0.1, 100)
	cam.SetPosition(glm.Vec3f{0, 0, 2})
	if red, _, _ := renderCube(t, r, scene, cam); red < 200 {
		t.Fatalf("first frame: want red, got r=%d", red)
	}

	// Force several grows of the store. first's record moves to a new device buffer
	// without ever being touched again.
	st := first.store
	for st.cap < 16 {
		extra := r.NewBasicMaterial()
		defer extra.Release()
	}

	if red, _, _ := renderCube(t, r, scene, cam); red < 200 {
		t.Fatalf("after grow: record was not re-uploaded, want red, got r=%d", red)
	}
}

// TestMaterialTextureRefsAreHeldByTheHandle: the store no longer tracks textures, so
// the material itself must keep them alive — including through Copy, which is what a
// Mesh holds. Releasing the authoring handle must not free a texture the mesh's copy
// still references.
func TestMaterialTextureRefsAreHeldByTheHandle(t *testing.T) {
	r, err := NewOffscreenRenderer(16, 16)
	if err != nil {
		t.Fatal(err)
	}
	defer r.Destroy()

	tex := r.TextureStore.Create([]byte{255, 255, 255, 255}, 1, 1, textures.Linear)
	mat := r.NewPBRMaterial()
	mat.SetColorMap(tex)
	tex.Release() // the material holds the only remaining reference

	held := mat.ColorMap()
	if !held.Valid() {
		t.Fatal("material did not keep its color map alive")
	}

	meshCopy := mat.Copy() // what Scene.NewMesh stores
	mat.Release()
	if !held.Valid() {
		t.Fatal("releasing the authoring handle freed a texture the mesh copy still holds")
	}
	meshCopy.Release()
	if held.Valid() {
		t.Fatal("texture outlived every material handle referencing it")
	}
}
