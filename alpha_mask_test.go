package pix_test

import (
	"testing"

	"github.com/bluescreen10/pix"
	"github.com/bluescreen10/pix/colors"
	"github.com/bluescreen10/pix/glm"
	"github.com/bluescreen10/pix/materials"
	"github.com/bluescreen10/pix/scenes"
	"github.com/bluescreen10/pix/textures"
)

// maskedPlate is a renderer and a scene with a red plate whose colour map's alpha is 0.2
// everywhere: masked at a cut-off above that, the whole plate has no surface.
type maskedPlate struct {
	r     *pix.Renderer
	scene *scenes.Scene
	cam   scenes.Camera
	plate *materials.PBRMaterial
}

func newMaskedPlate(t *testing.T) maskedPlate {
	t.Helper()
	r, err := pix.NewOffscreenRenderer(32, 32)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(r.Destroy)
	scene := scenes.New()
	t.Cleanup(scene.Destroy)
	scene.SetAmbient(colors.RGB32F{1, 1, 1}, 1)

	plate := r.NewPBRMaterial()
	plate.SetColor(colors.RGBA32F{1, 0, 0, 1})
	plate.SetRoughness(1)
	plate.SetMetallic(0)
	faint := r.TextureStore.Create(nrgbaImage([]byte{255, 255, 255, 51}, 1, 1), textures.SRGB)
	t.Cleanup(faint.Release)
	plate.SetColorMap(faint)

	cam := scene.NewPerspectiveCamera(45, 1, 0.1, 100)
	return maskedPlate{r: r, scene: scene, cam: cam, plate: plate}
}

// centerColor renders and returns the centre pixel's red and green, as bytes.
func (m maskedPlate) centerColor() (red, green byte) {
	for range 3 {
		m.r.Render(m.scene)
	}
	p := m.r.Pixels()[(16*32+16)*4:]
	return p[0], p[1]
}

// TestMaskedMaterialHasNoSurfaceWhereCutOut: a masked plate in front of a green wall
// shows the wall wherever its alpha is below the cut-off — shaded with and without a
// depth prepass — and the plate itself once it is not masked.
func TestMaskedMaterialHasNoSurfaceWhereCutOut(t *testing.T) {
	for _, prepass := range []bool{false, true} {
		name := "no prepass"
		if prepass {
			name = "prepass"
		}
		t.Run(name, func(t *testing.T) {
			m := newMaskedPlate(t)
			m.r.EnableDepthPrepass(prepass)
			wall := m.r.NewPBRMaterial()
			wall.SetColor(colors.RGBA32F{0, 1, 0, 1})
			wall.SetMetallic(0)
			wallMesh := m.scene.NewMesh(m.r.NewBoxGeometry(10, 10, 0.1), wall)
			wallMesh.SetPosition(glm.Vec3f{0, 0, -3})
			m.scene.NewMesh(m.r.NewBoxGeometry(2, 2, 0.05), m.plate)
			m.cam.SetPosition(glm.Vec3f{0, 0, 3})
			m.cam.LookAt(glm.Vec3f{})

			if red, green := m.centerColor(); red <= green {
				t.Fatalf("unmasked plate: centre = red %d green %d, want the red plate", red, green)
			}
			m.plate.SetAlphaCutoff(0.5)
			if red, green := m.centerColor(); green <= red {
				t.Errorf("plate masked out: centre = red %d green %d, want the green wall behind it", red, green)
			}
			m.plate.SetAlphaCutoff(0.1)
			if red, green := m.centerColor(); red <= green {
				t.Errorf("plate masked below its alpha: centre = red %d green %d, want the red plate", red, green)
			}
		})
	}
}

// TestMaskedMaterialCastsNoShadowWhereCutOut: a masked plate held over a white floor,
// under a sun straight above, leaves the floor beneath it lit where its alpha is below
// the cut-off, and shadows it once it is not masked.
func TestMaskedMaterialCastsNoShadowWhereCutOut(t *testing.T) {
	m := newMaskedPlate(t)
	m.r.EnableShadows(true)
	m.scene.SetAmbient(colors.RGB32F{}, 0)
	sun := m.scene.AddDirectionalLight(glm.Vec3f{0, -1, 0}, colors.RGB32F{1, 1, 1}, 2)
	sun.SetCastShadow(true)
	floor := m.r.NewPBRMaterial()
	floor.SetMetallic(0)
	floor.SetRoughness(1)
	m.scene.NewMesh(m.r.NewPlaneGeometry(20, 20, 1, 1), floor)
	plateMesh := m.scene.NewMesh(m.r.NewBoxGeometry(4, 0.05, 4), m.plate)
	plateMesh.SetPosition(glm.Vec3f{0, 1, 0})
	// Looking at the floor under the plate's edge from below its height, so the plate
	// itself is out of view.
	m.cam.SetPosition(glm.Vec3f{0, 0.5, 3})
	m.cam.LookAt(glm.Vec3f{0, 0, 0})

	shadowed, _ := m.centerColor()
	m.plate.SetAlphaCutoff(0.5)
	lit, _ := m.centerColor()
	if lit <= shadowed+40 {
		t.Errorf("floor under the plate = %d masked out, %d whole; want it lit once the plate is masked out", lit, shadowed)
	}
}
