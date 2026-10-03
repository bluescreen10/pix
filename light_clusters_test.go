package pix_test

import (
	"testing"

	"github.com/bluescreen10/pix"
	"github.com/bluescreen10/pix/colors"
	"github.com/bluescreen10/pix/glm"
	"github.com/bluescreen10/pix/scenes"
	"github.com/chewxy/math32"
)

const clusterTestSize = 192

// floorScene is a white, matte floor at y = 0 with no ambient light, seen obliquely so
// it runs across the screen and far into the distance: every light cluster's tile, and
// many of its depth slices, cover part of it.
func floorScene(t *testing.T) (*pix.Renderer, *scenes.Scene, scenes.Camera) {
	t.Helper()
	r, err := pix.NewOffscreenRenderer(clusterTestSize, clusterTestSize)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(r.Destroy)
	r.SetClearColor(colors.RGBA32F{0, 0, 0, 1})

	scene := scenes.New()
	t.Cleanup(scene.Destroy)
	scene.SetAmbient(colors.RGB32F{})
	material := r.NewBlinnPhongMaterial()
	material.SetSpecular(0)
	scene.Add(scene.NewMesh(r.NewPlaneGeometry(200, 200, 1, 1), material))

	cam := scene.NewPerspectiveCamera(60, 1, 0.1, 100)
	scene.Add(cam)
	cam.SetPosition(glm.Vec3f{1, 5, 9})
	cam.LookAt(glm.Vec3f{0, 0, -3})
	return r, scene, cam
}

// floorPoints returns where on the floor each pixel of the camera's image lands, and
// whether it lands on the floor at all.
func floorPoints(cam scenes.Camera) ([]glm.Vec3f, []bool) {
	inverse := cam.ViewProjection().Inv()
	unproject := func(ndc glm.Vec4f) glm.Vec3f {
		p := inverse.Mul4x1(ndc)
		return p.Vec3().Scale(1 / p[3])
	}

	points := make([]glm.Vec3f, clusterTestSize*clusterTestSize)
	onFloor := make([]bool, len(points))
	for y := range clusterTestSize {
		for x := range clusterTestSize {
			// The image's first row is the top of the view.
			ndcX := (float32(x)+0.5)/clusterTestSize*2 - 1
			ndcY := 1 - (float32(y)+0.5)/clusterTestSize*2
			near := unproject(glm.Vec4f{ndcX, ndcY, 1, 1})
			far := unproject(glm.Vec4f{ndcX, ndcY, 0, 1})
			if near[1] <= 0 || far[1] >= 0 {
				continue
			}
			i := y*clusterTestSize + x
			points[i] = near.Add(far.Sub(near).Scale(near[1] / (near[1] - far[1])))
			onFloor[i] = true
		}
	}
	return points, onFloor
}

// checkLitWhereReached renders the scene and checks, pixel by pixel, that the floor is
// lit wherever some light reaches it by a clear margin, and black wherever none
// reaches it at all. reach reports how far inside a light's reach a floor point is: 1
// at the light, 0 at the edge of what it lights, negative beyond.
func checkLitWhereReached(t *testing.T, r *pix.Renderer, scene *scenes.Scene, cam scenes.Camera, reach func(p glm.Vec3f) float32) {
	t.Helper()
	r.Render(scene)
	pixels := r.Pixels()
	points, onFloor := floorPoints(cam)

	var lit, dark, unlitInside, litOutside int
	for i, p := range points {
		if !onFloor[i] {
			continue
		}
		value := max(pixels[i*4], pixels[i*4+1], pixels[i*4+2])
		switch inside := reach(p); {
		case inside > 0.05:
			lit++
			if value < 8 {
				unlitInside++
			}
		case inside < -0.05:
			dark++
			if value > 1 {
				litOutside++
			}
		}
	}
	if lit == 0 || dark == 0 {
		t.Fatalf("the floor shows %d pixels inside a light's reach and %d outside, want some of each", lit, dark)
	}
	if unlitInside > 0 {
		t.Errorf("%d of %d floor pixels inside a light's reach are unlit, want 0", unlitInside, lit)
	}
	if litOutside > 0 {
		t.Errorf("%d of %d floor pixels outside every light's reach are lit, want 0", litOutside, dark)
	}
}

// TestEveryPointLightLightsItsReach puts many more point lights than a pixel could
// afford to loop over across a floor, each with a short range, and checks that every
// one of them lights the floor out to its range, and nothing beyond it. A light the
// clusters failed to list where it reaches leaves a dark hole; one listed where it does
// not reach changes nothing, so the second half only checks the lights themselves.
func TestEveryPointLightLightsItsReach(t *testing.T) {
	r, scene, cam := floorScene(t)

	const lightRange, lightHeight = 1.2, 0.3
	var lights []glm.Vec3f
	for z := float32(-24); z <= 6; z += 2 {
		for x := float32(-8); x <= 8; x += 2 {
			lights = append(lights, glm.Vec3f{x, lightHeight, z})
		}
	}
	for _, p := range lights {
		scene.AddPointLight(p, colors.RGB32F{1, 1, 1}, 40, lightRange)
	}

	checkLitWhereReached(t, r, scene, cam, func(p glm.Vec3f) float32 {
		nearest := float32(math32.Inf(1))
		for _, light := range lights {
			nearest = min(nearest, light.Sub(p).Length())
		}
		return 1 - nearest/lightRange
	})
}

// TestSpotLightLightsItsCone points spot lights straight down at the floor, narrow and
// wide, and checks each lights the disc its cone cuts out of the floor and nothing
// else. A spot light is listed in the cells its cone reaches rather than in every cell
// its range does, so a bound cut too tight shows up as a cone with an edge missing.
// Each light hangs just short of its range above the floor, where a cone's bound is at
// its tightest.
func TestSpotLightLightsItsCone(t *testing.T) {
	r, scene, cam := floorScene(t)

	type spot struct {
		position          glm.Vec3f
		angle, lightRange float32
	}
	spots := []spot{
		{glm.Vec3f{-3, 2.4, 0}, 0.35, 2.6},
		{glm.Vec3f{2, 2, -2}, 0.9, 3.5},
		{glm.Vec3f{0, 1, -9}, 1.3, 6},
	}
	for _, s := range spots {
		scene.AddSpotLight(s.position, glm.Vec3f{0, -1, 0}, colors.RGB32F{1, 1, 1}, 100, s.lightRange, s.angle, 0)
	}

	checkLitWhereReached(t, r, scene, cam, func(p glm.Vec3f) float32 {
		best := float32(-1)
		for _, s := range spots {
			d := p.Sub(s.position)
			// How far inside the cone, and inside the range, as fractions of each.
			cone := 1 - math32.Acos(-d[1]/d.Length())/s.angle
			reach := 1 - d.Length()/s.lightRange
			best = max(best, min(cone, reach))
		}
		return best
	})
}
