package pix

import (
	"math"
	"testing"

	"github.com/bluescreen10/pix/glm"
)

func rotY(a float32) glm.Mat4f {
	c, s := float32(math.Cos(float64(a))), float32(math.Sin(float64(a)))
	m := glm.Mat4Identity[float32]()
	m[0], m[8] = c, s
	m[2], m[10] = -s, c
	return m
}

// normalCube returns a cube with 24 vertices (4 per face) so each face carries a
// constant outward normal — required for per-face lighting to vary.
func normalCube() Data {
	type face struct {
		n       glm.Vec3f
		corners [4]glm.Vec3f
	}
	h := float32(0.5)
	faces := []face{
		{glm.Vec3f{1, 0, 0}, [4]glm.Vec3f{{h, -h, -h}, {h, h, -h}, {h, h, h}, {h, -h, h}}},
		{glm.Vec3f{-1, 0, 0}, [4]glm.Vec3f{{-h, -h, h}, {-h, h, h}, {-h, h, -h}, {-h, -h, -h}}},
		{glm.Vec3f{0, 1, 0}, [4]glm.Vec3f{{-h, h, -h}, {-h, h, h}, {h, h, h}, {h, h, -h}}},
		{glm.Vec3f{0, -1, 0}, [4]glm.Vec3f{{-h, -h, h}, {-h, -h, -h}, {h, -h, -h}, {h, -h, h}}},
		{glm.Vec3f{0, 0, 1}, [4]glm.Vec3f{{-h, -h, h}, {h, -h, h}, {h, h, h}, {-h, h, h}}},
		{glm.Vec3f{0, 0, -1}, [4]glm.Vec3f{{h, -h, -h}, {-h, -h, -h}, {-h, h, -h}, {h, h, -h}}},
	}
	var d Data
	for _, f := range faces {
		base := uint32(len(d.Positions))
		for _, c := range f.corners {
			d.Positions = append(d.Positions, c)
			d.Normals = append(d.Normals, f.n)
		}
		d.Indices = append(d.Indices, base, base+1, base+2, base, base+2, base+3)
	}
	return d
}

func TestDirectionalLighting(t *testing.T) {
	const size = 160
	r, err := NewOffscreenRenderer(size, size)
	if err != nil {
		t.Fatal(err)
	}
	defer r.Destroy()
	scene := r.NewScene()
	defer scene.Destroy()

	cube := r.NewGeometry(normalCube())
	mat := r.NewBlinnPhongMaterial()
	m := scene.NewMesh(cube, mat)
	m.SetRotationQuat(glm.NewQuat(float32(0.6), glm.Vec3f{0, 1, 0})) // rotate so +X and +Z faces both show
	scene.Add(m)
	scene.SetAmbient(glm.Vec3f{0.1, 0.1, 0.1})

	cam := NewCamera()
	cam.Position = glm.Vec3f{0, 0.6, 3}
	cam.Far = 100

	// The renderer gamma-encodes output to sRGB; decode back to linear so the
	// thresholds match the (linear) lighting math.
	srgbToLinear := func(v byte) float64 {
		c := float64(v) / 255
		if c <= 0.04045 {
			return c / 12.92
		}
		return math.Pow((c+0.055)/1.055, 2.4)
	}
	render := func() (meanLum, maxLum float64) {
		r.Render(scene, cam)
		px := r.Pixels()
		var sum, n float64
		for i := 0; i < len(px); i += 4 {
			if px[i] == 0 && px[i+1] == 0 && px[i+2] == 0 {
				continue
			}
			lum := 0.299*srgbToLinear(px[i]) + 0.587*srgbToLinear(px[i+1]) + 0.114*srgbToLinear(px[i+2])
			sum += lum
			n++
			if lum > maxLum {
				maxLum = lum
			}
		}
		if n > 0 {
			meanLum = sum / n
		}
		return
	}

	ambMean, ambMax := render()

	scene.AddDirectionalLight(glm.Vec3f{-1, -0.2, -0.5}, glm.Vec3f{1, 1, 1}, 1.0)
	litMean, litMax := render()

	t.Logf("ambient(mean=%.3f max=%.3f) -> lit(mean=%.3f max=%.3f)", ambMean, ambMax, litMean, litMax)
	if ambMax > 0.2 {
		t.Fatalf("ambient-only too bright (max=%.3f), expected ~0.1", ambMax)
	}
	if litMean < ambMean*1.5 {
		t.Fatalf("directional light added little (ambMean=%.3f litMean=%.3f)", ambMean, litMean)
	}
	if litMax < 0.5 {
		t.Fatalf("lit face not bright enough (max=%.3f) — N·L or normals wrong", litMax)
	}
}
