package pix_test

import (
	"math"
	"testing"

	"github.com/bluescreen10/pix"
	"github.com/bluescreen10/pix/colors"
	"github.com/bluescreen10/pix/glm"
	"github.com/bluescreen10/pix/materials"
	"github.com/bluescreen10/pix/scenes"
)

// renderPBRHeadOn renders a ground at y = 0 in a black, non-metal PBR material set up
// by configure, with no ambient light, under a sun shining straight down, seen straight
// down, and returns the middle pixel. Black scatters nothing, so all that pixel shows is
// the sun's reflection, seen head-on.
func renderPBRHeadOn(t *testing.T, configure func(*materials.PBRMaterial)) [3]byte {
	t.Helper()
	r, err := pix.NewOffscreenRenderer(postSize, postSize)
	if err != nil {
		t.Fatal(err)
	}
	defer r.Destroy()
	scene := scenes.New()
	defer scene.Destroy()
	scene.SetAmbient(colors.RGB32F{}, 0)
	scene.AddDirectionalLight(glm.Vec3f{0, -1, 0}, colors.RGB32F{1, 1, 1}, 4)

	material := r.NewPBRMaterial()
	defer material.Release()
	material.SetColor(colors.RGBA32F{0, 0, 0, 1})
	material.SetMetallic(0)
	material.SetRoughness(0.4)
	configure(material)
	scene.NewMesh(r.NewPlaneGeometry(40, 40, 1, 1), material)
	cam := scene.NewOrthographicCamera(-4, 4, -4, 4, 0.1, 100)
	cam.SetPosition(glm.Vec3f{0, 10, 0.001})
	cam.LookAt(glm.Vec3f{})

	r.Render(scene)
	return pixelAt(r, postSize/2, postSize/2)
}

// TestSpecularIntensityScalesReflection: a black dielectric shows the sun's reflection,
// and none of it with a specular intensity of 0 (KHR_materials_specular).
func TestSpecularIntensityScalesReflection(t *testing.T) {
	reflecting := renderPBRHeadOn(t, func(m *materials.PBRMaterial) {})
	if reflecting[0] < 20 {
		t.Fatalf("black dielectric at intensity 1 = %v, want the sun's reflection", reflecting)
	}
	matte := renderPBRHeadOn(t, func(m *materials.PBRMaterial) {
		m.SetSpecularIntensity(0)
	})
	if matte != ([3]byte{}) {
		t.Errorf("black dielectric at intensity 0 = %v, want black: it reflects nothing", matte)
	}
}

// TestSpecularColorTintsReflection: seen head-on, a dielectric's reflection is its
// specular colour — red reflects only red (KHR_materials_specular).
func TestSpecularColorTintsReflection(t *testing.T) {
	got := renderPBRHeadOn(t, func(m *materials.PBRMaterial) {
		m.SetSpecularColor(colors.RGB32F{1, 0, 0})
	})
	if got[0] < 20 || got[1] > 2 || got[2] > 2 {
		t.Errorf("head-on reflection with a red specular colour = %v, want red alone", got)
	}
}

// TestClearcoatReflects: a black surface that reflects nothing of its own shows the sun's
// reflection off a clear coat over it, and none once the coat is gone
// (KHR_materials_clearcoat).
func TestClearcoatReflects(t *testing.T) {
	coated := renderPBRHeadOn(t, func(m *materials.PBRMaterial) {
		m.SetSpecularIntensity(0)
		m.SetClearcoat(1)
		m.SetClearcoatRoughness(0.4)
	})
	if coated[0] < 20 {
		t.Errorf("coated black surface = %v, want the sun's reflection off the coat", coated)
	}
	bare := renderPBRHeadOn(t, func(m *materials.PBRMaterial) {
		m.SetSpecularIntensity(0)
		m.SetClearcoatRoughness(0.4)
	})
	if bare != ([3]byte{}) {
		t.Errorf("uncoated black surface = %v, want black", bare)
	}
}

// TestClearcoatDrawsBesideUncoated: materials of one pool with and without a clear coat
// are drawn through pipelines of their own, in one frame — and a material that gains a
// coat after it was first drawn moves to the coated one.
func TestClearcoatDrawsBesideUncoated(t *testing.T) {
	r, err := pix.NewOffscreenRenderer(postSize, postSize)
	if err != nil {
		t.Fatal(err)
	}
	defer r.Destroy()
	scene := scenes.New()
	defer scene.Destroy()
	scene.SetAmbient(colors.RGB32F{}, 0)
	scene.AddDirectionalLight(glm.Vec3f{0, -1, 0}, colors.RGB32F{1, 1, 1}, 4)

	newBlack := func(clearcoat float32, x float32) *materials.PBRMaterial {
		m := r.NewPBRMaterial()
		m.SetColor(colors.RGBA32F{0, 0, 0, 1})
		m.SetMetallic(0)
		m.SetSpecularIntensity(0)
		m.SetClearcoat(clearcoat)
		m.SetClearcoatRoughness(0.4)
		ground := scene.NewMesh(r.NewPlaneGeometry(4, 8, 1, 1), m)
		ground.SetPosition(glm.Vec3f{x, 0, 0})
		return m
	}
	coated := newBlack(1, -2)
	defer coated.Release()
	bare := newBlack(0, 2)
	defer bare.Release()
	cam := scene.NewOrthographicCamera(-4, 4, -4, 4, 0.1, 100)
	cam.SetPosition(glm.Vec3f{0, 10, 0.001})
	cam.LookAt(glm.Vec3f{})

	// The probes sit a quarter of the frame from each edge, one over each plate, their
	// pixel centres mirrored about the frame's centre so that both see the sun at the
	// same angle. Which is which on screen is the camera's business, so the coated one
	// is found by looking.
	probes := [2][2]int{{postSize / 4, postSize / 2}, {postSize - 1 - postSize/4, postSize / 2}}
	render := func() (brighter, darker byte) {
		r.Render(scene)
		a, b := pixelAt(r, probes[0][0], probes[0][1])[0], pixelAt(r, probes[1][0], probes[1][1])[0]
		return max(a, b), min(a, b)
	}

	if brighter, darker := render(); brighter < 20 || darker != 0 {
		t.Errorf("coated and bare plate = %d and %d, want the coat's reflection and black", brighter, darker)
	}
	bare.SetClearcoat(1)
	if brighter, darker := render(); darker < 20 || !isWithin(brighter, darker, 2) {
		t.Errorf("both plates coated = %d and %d, want both the coat's reflection", brighter, darker)
	}
}

// TestSmoothCurveShowsHighlight: a mirror-smooth sphere still shows the sun's highlight.
// A point of light reflects off a perfect mirror into a point no pixel catches, but a
// pixel of a curved surface covers a spread of normals, one of which faces the sun: the
// highlight is widened to what the pixel covers (see antiAliasedRoughness).
func TestSmoothCurveShowsHighlight(t *testing.T) {
	r, err := pix.NewOffscreenRenderer(postSize, postSize)
	if err != nil {
		t.Fatal(err)
	}
	defer r.Destroy()
	r.SetClearColor(colors.RGBA32F{0, 0, 0, 1})
	scene := scenes.New()
	defer scene.Destroy()
	scene.SetAmbient(colors.RGB32F{}, 0)
	scene.AddDirectionalLight(glm.Vec3f{0, 0, -1}, colors.RGB32F{1, 1, 1}, 1)

	mirror := r.NewPBRMaterial()
	defer mirror.Release()
	mirror.SetColor(colors.RGBA32F{0, 0, 0, 1})
	mirror.SetMetallic(0)
	mirror.SetRoughness(0)
	sphere := scene.NewMesh(r.NewSphereGeometry(1, 48, 32), mirror)
	sphere.SetPosition(glm.Vec3f{0, 0, -3})
	cam := scene.NewPerspectiveCamera(45, 1, 0.1, 100)
	cam.LookAt(glm.Vec3f{0, 0, -1})

	r.Render(scene)
	var brightest byte
	px := r.Pixels()
	for i := 0; i < len(px); i += 4 {
		brightest = max(brightest, px[i])
	}
	if brightest < 100 {
		t.Errorf("brightest pixel of a mirror-smooth sphere under the sun = %d, want its highlight", brightest)
	}
}

// TestSheenGlowsAtTheRim: a black sphere with a sheen glows at its rim, where its
// surface turns edge-on to the eye, and hardly at its centre — under a flat ambient
// light, and under a sun behind the eye — and not at all without the sheen
// (KHR_materials_sheen).
func TestSheenGlowsAtTheRim(t *testing.T) {
	t.Run("ambient", func(t *testing.T) {
		testSheenGlowsAtTheRim(t, func(scene *scenes.Scene) {
			scene.SetAmbient(colors.RGB32F{1, 1, 1}, 1)
		})
	})
	t.Run("sun", func(t *testing.T) {
		testSheenGlowsAtTheRim(t, func(scene *scenes.Scene) {
			scene.SetAmbient(colors.RGB32F{}, 0)
			scene.AddDirectionalLight(glm.Vec3f{0, 0, -1}, colors.RGB32F{1, 1, 1}, 4)
		})
	})
}

func testSheenGlowsAtTheRim(t *testing.T, light func(*scenes.Scene)) {
	render := func(sheen colors.RGB32F) (centre, rim byte) {
		r, err := pix.NewOffscreenRenderer(postSize, postSize)
		if err != nil {
			t.Fatal(err)
		}
		defer r.Destroy()
		r.SetClearColor(colors.RGBA32F{0, 0, 0, 1})
		scene := scenes.New()
		defer scene.Destroy()
		light(scene)

		velvet := r.NewPBRMaterial()
		defer velvet.Release()
		velvet.SetColor(colors.RGBA32F{0, 0, 0, 1})
		velvet.SetSpecularIntensity(0)
		velvet.SetSheenColor(sheen)
		velvet.SetSheenRoughness(0.5)
		sphere := scene.NewMesh(r.NewSphereGeometry(1, 48, 32), velvet)
		sphere.SetPosition(glm.Vec3f{0, 0, -3})
		cam := scene.NewPerspectiveCamera(45, 1, 0.1, 100)
		cam.LookAt(glm.Vec3f{0, 0, -1})

		r.Render(scene)
		for x := range postSize {
			rim = max(rim, pixelAt(r, x, postSize/2)[0])
		}
		return pixelAt(r, postSize/2, postSize/2)[0], rim
	}

	centre, rim := render(colors.RGB32F{1, 1, 1})
	if rim < 60 || int(rim) < 2*int(centre) {
		t.Errorf("velvet sphere's centre = %d, brightest of its middle row = %d, want a rim at least twice as bright", centre, rim)
	}
	if centre, rim := render(colors.RGB32F{}); centre != 0 || rim != 0 {
		t.Errorf("black sphere without a sheen: centre = %d, rim = %d, want black", centre, rim)
	}
}

// TestAnisotropyStretchesTheHighlight: the sun's highlight on a flat metal plate seen
// from above is round, and with anisotropy stretched along one axis of the plate — the
// other one once the anisotropy is turned a quarter turn (KHR_materials_anisotropy).
func TestAnisotropyStretchesTheHighlight(t *testing.T) {
	// highlightExtent renders the plate and returns how many pixels of the middle row,
	// and of the middle column, are lit above half of the brightest.
	highlightExtent := func(anisotropy, rotation float32) (row, column int) {
		r, err := pix.NewOffscreenRenderer(postSize, postSize)
		if err != nil {
			t.Fatal(err)
		}
		defer r.Destroy()
		scene := scenes.New()
		defer scene.Destroy()
		scene.SetAmbient(colors.RGB32F{}, 0)
		scene.AddDirectionalLight(glm.Vec3f{0, -1, 0}, colors.RGB32F{1, 1, 1}, 0.5)

		brushed := r.NewPBRMaterial()
		defer brushed.Release()
		brushed.SetColor(colors.RGBA32F{1, 1, 1, 1})
		brushed.SetMetallic(1)
		brushed.SetRoughness(0.3)
		brushed.SetAnisotropy(anisotropy)
		brushed.SetAnisotropyRotation(rotation)
		scene.NewMesh(r.NewPlaneGeometry(40, 40, 1, 1), brushed)
		cam := scene.NewOrthographicCamera(-4, 4, -4, 4, 0.1, 100)
		cam.SetPosition(glm.Vec3f{0, 3, 0.001})
		cam.LookAt(glm.Vec3f{})
		r.Render(scene)

		var brightest byte
		for i := range postSize {
			brightest = max(brightest, pixelAt(r, i, postSize/2)[0], pixelAt(r, postSize/2, i)[0])
		}
		for i := range postSize {
			if pixelAt(r, i, postSize/2)[0] > brightest/2 {
				row++
			}
			if pixelAt(r, postSize/2, i)[0] > brightest/2 {
				column++
			}
		}
		return row, column
	}

	row, column := highlightExtent(0, 0)
	if !isWithin(byte(row), byte(column), 2) {
		t.Errorf("isotropic highlight spans %d pixels across, %d down, want it round", row, column)
	}
	stretchedRow, stretchedColumn := highlightExtent(1, 0)
	turnedRow, turnedColumn := highlightExtent(1, math.Pi/2)
	if max(stretchedRow, stretchedColumn) < 2*min(stretchedRow, stretchedColumn) {
		t.Errorf("anisotropic highlight spans %d pixels across, %d down, want it stretched along one", stretchedRow, stretchedColumn)
	}
	if (stretchedRow > stretchedColumn) == (turnedRow > turnedColumn) {
		t.Errorf("anisotropic highlight spans %dx%d, turned a quarter turn %dx%d, want it stretched along the other axis", stretchedRow, stretchedColumn, turnedRow, turnedColumn)
	}
}
