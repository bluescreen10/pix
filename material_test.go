package pix_test

import (
	"testing"

	"github.com/bluescreen10/pix"
	"github.com/bluescreen10/pix/colors"
	"github.com/bluescreen10/pix/geometries"
	"github.com/bluescreen10/pix/glm"
	"github.com/bluescreen10/pix/materials"
	"github.com/bluescreen10/pix/scenes"
)

// TestMaterialClasses renders the same geometry with three different material
// classes side by side and checks each pipeline produces distinct shading: the
// unlit surface is uniformly bright (ignores lights), while the lit ones darken on
// faces angled away from the light. Exercises per-batch pipeline selection.
func TestMaterialClasses(t *testing.T) {
	const size = 240
	r, err := pix.NewOffscreenRenderer(size, size)
	if err != nil {
		t.Fatal(err)
	}
	defer r.Destroy()
	scene := scenes.New()
	defer scene.Destroy()

	cube := r.GeometryStore.Create(normalCube())
	basic := r.NewBasicMaterial()
	basic.SetColor(colors.RGBA32F{0.8, 0.8, 0.8, 1})
	phong := r.NewBlinnPhongMaterial()
	phong.SetColor(colors.RGBA32F{0.8, 0.8, 0.8, 1})
	pbr := r.NewPBRMaterial()
	pbr.SetColor(colors.RGBA32F{0.8, 0.8, 0.8, 1})

	place := func(mat materials.Material, x float32) {
		m := scene.NewMesh(cube, mat)
		m.SetPosition(glm.Vec3f{x, 0, 0})
		m.SetRotationQuat(glm.NewQuat(float32(0.6), glm.Vec3f{0, 1, 0}))
	}
	place(basic, -1.4)
	place(phong, 0)
	place(pbr, 1.4)

	scene.SetAmbient(colors.RGB32F{0.15, 0.15, 0.15}, 1)
	scene.AddDirectionalLight(glm.Vec3f{-1, -0.3, -0.6}, colors.RGB32F{1, 1, 1}, 1.0)

	cam := scene.NewPerspectiveCamera(45, 1, 0.1, 1000)
	cam.SetPosition(glm.Vec3f{0, 0.5, 5})
	cam.LookAt(glm.Vec3f{})

	r.Render(scene)

	// Split the frame into thirds (basic | phong | pbr) and measure, for each, the
	// spread between the brightest and darkest lit pixel. Unlit is flat (small
	// spread); the lit materials shade across faces (larger spread).
	px := r.Pixels()
	third := size / 3
	spread := func(x0, x1 int) (float64, int) {
		lo, hi := 1.0, 0.0
		lit := 0
		for y := 0; y < size; y++ {
			for x := x0; x < x1; x++ {
				i := (y*size + x) * 4
				rr, gg, bb := px[i], px[i+1], px[i+2]
				if rr == 0 && gg == 0 && bb == 0 {
					continue
				}
				lit++
				l := (0.299*float64(rr) + 0.587*float64(gg) + 0.114*float64(bb)) / 255
				if l < lo {
					lo = l
				}
				if l > hi {
					hi = l
				}
			}
		}
		return hi - lo, lit
	}

	basicSpread, basicLit := spread(0, third)
	phongSpread, phongLit := spread(third, 2*third)
	pbrSpread, pbrLit := spread(2*third, size)
	t.Logf("basic: spread=%.3f lit=%d | phong: spread=%.3f lit=%d | pbr: spread=%.3f lit=%d",
		basicSpread, basicLit, phongSpread, phongLit, pbrSpread, pbrLit)

	for name, lit := range map[string]int{"basic": basicLit, "phong": phongLit, "pbr": pbrLit} {
		if lit < 300 {
			t.Fatalf("%s cube barely rendered (%d px) — pipeline/material wrong", name, lit)
		}
	}
	// The unlit cube should be markedly flatter than the lit ones.
	if basicSpread > phongSpread || basicSpread > pbrSpread {
		t.Fatalf("unlit spread %.3f not smaller than lit (phong %.3f, pbr %.3f) — classes not distinct",
			basicSpread, phongSpread, pbrSpread)
	}
}

// TestDoubleSidedBackFaceIsLit renders a double-sided quad from in front and from
// behind, lit each time from the side it is seen from. A double-sided surface is lit
// on the side it is seen from, as glTF asks: its back face reverses the normal, and
// comes out as bright as its front. Without that the back face turns its normal away
// from both the light and the camera, and stays dark — or, on glass, whose back faces
// show through its front ones, reflects as though seen at a grazing angle.
func TestDoubleSidedBackFaceIsLit(t *testing.T) {
	const size = 64
	r, err := pix.NewOffscreenRenderer(size, size)
	if err != nil {
		t.Fatal(err)
	}
	defer r.Destroy()
	r.SetClearColor(colors.RGBA32F{0, 0, 0, 1})

	quad := r.GeometryStore.Create(geometries.GeometryConfig{
		Attributes: []geometries.Attribute{
			geometries.NewAttribute(geometries.AttributePosition, geometries.Float32x3, []glm.Vec3f{{-0.8, -0.8, 0}, {0.8, -0.8, 0}, {0.8, 0.8, 0}, {-0.8, 0.8, 0}}),
			geometries.NewAttribute(geometries.AttributeNormal, geometries.Float32x3, []glm.Vec3f{{0, 0, 1}, {0, 0, 1}, {0, 0, 1}, {0, 0, 1}}),
		},
		Indices: []uint32{0, 1, 2, 0, 2, 3},
	})
	// renderCenter views the quad from side (+1 in front, -1 behind), lit from behind
	// the camera, and returns the centre pixel's red channel.
	renderCenter := func(material materials.Material, side float32) uint8 {
		scene := scenes.New()
		defer scene.Destroy()
		scene.SetAmbient(colors.RGB32F{}, 1)
		scene.AddDirectionalLight(glm.Vec3f{0, 0, -side}, colors.RGB32F{1, 1, 1}, 2)
		scene.NewMesh(quad, material)
		cam := scene.NewPerspectiveCamera(45, 1, 0.1, 100)
		cam.SetPosition(glm.Vec3f{0, 0, 2 * side})
		cam.LookAt(glm.Vec3f{})
		r.Render(scene)
		return r.Pixels()[(size/2*size+size/2)*4]
	}

	pbr := r.NewPBRMaterial()
	pbr.SetColor(colors.RGBA32F{1, 1, 1, 1})
	pbr.SetMetallic(0)
	pbr.SetRoughness(1)
	pbr.SetDoubleSided(true)
	blinnPhong := r.NewBlinnPhongMaterial()
	blinnPhong.SetColor(colors.RGBA32F{1, 1, 1, 1})
	blinnPhong.SetDoubleSided(true)

	for _, tc := range []struct {
		name     string
		material materials.Material
	}{
		{"PBR", pbr},
		{"BlinnPhong", blinnPhong},
	} {
		t.Run(tc.name, func(t *testing.T) {
			front, back := renderCenter(tc.material, 1), renderCenter(tc.material, -1)
			if front < 100 {
				t.Fatalf("front = %d, too dim to compare against", front)
			}
			if diff := int(back) - int(front); diff < -3 || diff > 3 {
				t.Errorf("back = %d, want %d (the front, lit the same way)", back, front)
			}
		})
	}
}

// TestCullBackHidesTheBackFace renders a single-sided quad, wound counter-clockwise as
// glTF winds front faces, from in front and from behind. Culling back faces has to
// keep the side it faces and drop the other.
func TestCullBackHidesTheBackFace(t *testing.T) {
	const size = 64
	r, err := pix.NewOffscreenRenderer(size, size)
	if err != nil {
		t.Fatal(err)
	}
	defer r.Destroy()
	r.SetClearColor(colors.RGBA32F{0, 0, 0, 1})

	quad := r.GeometryStore.Create(geometries.GeometryConfig{
		Attributes: []geometries.Attribute{
			geometries.NewAttribute(geometries.AttributePosition, geometries.Float32x3, []glm.Vec3f{{-0.8, -0.8, 0}, {0.8, -0.8, 0}, {0.8, 0.8, 0}, {-0.8, 0.8, 0}}),
		},
		Indices: []uint32{0, 1, 2, 0, 2, 3},
	})
	material := r.NewBasicMaterial()
	material.SetColor(colors.RGBA32F{1, 1, 1, 1})
	material.SetCull(materials.CullBack)

	// renderCenter views the quad from side (+1 in front, -1 behind) and returns the
	// centre pixel's red channel.
	renderCenter := func(side float32) uint8 {
		scene := scenes.New()
		defer scene.Destroy()
		scene.NewMesh(quad, material)
		cam := scene.NewPerspectiveCamera(45, 1, 0.1, 100)
		cam.SetPosition(glm.Vec3f{0, 0, 2 * side})
		cam.LookAt(glm.Vec3f{})
		r.Render(scene)
		return r.Pixels()[(size/2*size+size/2)*4]
	}

	if front := renderCenter(1); front != 255 {
		t.Errorf("seen from in front: centre = %d, want 255 (the quad)", front)
	}
	if back := renderCenter(-1); back != 0 {
		t.Errorf("seen from behind: centre = %d, want 0 (culled)", back)
	}
}
