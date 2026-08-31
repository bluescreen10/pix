package pix

import (
	"testing"

	"github.com/bluescreen10/pix/glm"
)

// TestMaterialClasses renders the same geometry with three different material
// classes side by side and checks each pipeline produces distinct shading: the
// unlit surface is uniformly bright (ignores lights), while the lit ones darken on
// faces angled away from the light. Exercises per-batch pipeline selection.
func TestMaterialClasses(t *testing.T) {
	const size = 240
	r, err := NewOffscreenRenderer(size, size)
	if err != nil {
		t.Fatal(err)
	}
	defer r.Destroy()
	scene := r.NewScene()
	defer scene.Destroy()

	cube := r.NewGeometry(normalCube())
	basic := r.NewBasicMaterial()
	basic.SetColor(glm.RGBA32F{0.8, 0.8, 0.8, 1})
	phong := r.NewBlinnPhongMaterial()
	phong.SetColor(glm.RGBA32F{0.8, 0.8, 0.8, 1})
	pbr := r.NewPBRMaterial()
	pbr.SetColor(glm.RGBA32F{0.8, 0.8, 0.8, 1})

	place := func(mat Material, x float32) {
		m := scene.NewMesh(cube, mat)
		m.SetPosition(glm.Vec3f{x, 0, 0})
		m.SetRotationQuat(glm.NewQuat(float32(0.6), glm.Vec3f{0, 1, 0}))
		scene.Add(m)
	}
	place(basic, -1.4)
	place(phong, 0)
	place(pbr, 1.4)

	scene.SetAmbient(glm.Vec3f{0.15, 0.15, 0.15})
	scene.AddDirectionalLight(glm.Vec3f{-1, -0.3, -0.6}, glm.Vec3f{1, 1, 1}, 1.0)

	cam := NewCamera()
	cam.Position = glm.Vec3f{0, 0.5, 5}
	cam.FOV = 45

	r.Render(scene, cam)

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
