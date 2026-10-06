package pix_test

import (
	"testing"

	"github.com/bluescreen10/pix"
	"github.com/bluescreen10/pix/colors"
	"github.com/bluescreen10/pix/glm"
	"github.com/bluescreen10/pix/scenes"
)

// TestDepthPrepassMatchesShadingExactly is the defining property of a depth prepass:
// filling depth first must not change the image, only what it costs. The prepass runs a
// different vertex program from the shading pass, so the two have to agree on clip
// position bit for bit — a fragment landing a fraction of an ULP behind depth the
// prepass wrote fails the test and is dropped, punching holes in the surface.
//
// The rotation sweep is the whole point. An axis-aligned model matrix is exact, so both
// programs agree by luck and the test passes no matter how broken invariance is; only a
// real rotation makes the two roundings diverge. Before -fpreserve-invariance reached
// the Metal compile step this lost about a fifth of the frame at 0.9 radians while
// staying perfectly clean at 0.
func TestDepthPrepassMatchesShadingExactly(t *testing.T) {
	const size = 240
	r, err := pix.NewOffscreenRenderer(size, size)
	if err != nil {
		t.Fatal(err)
	}
	defer r.Destroy()

	scene := scenes.New()
	defer scene.Destroy()
	scene.SetAmbient(colors.RGB32F{0.4, 0.4, 0.4}, 1)
	scene.AddDirectionalLight(glm.Vec3f{-0.4, -1, -0.3}, colors.RGB32F{1, 1, 1}, 2)

	mat := r.NewPBRMaterial()
	box := scene.NewMesh(r.GeometryStore.Create(pix.BoxGeometry(40, 40, 40)), mat)
	box.SetPosition(glm.Vec3f{0, 0, -60})
	scene.Add(box)
	ground := scene.NewMesh(r.GeometryStore.Create(pix.BoxGeometry(4000, 2, 4000)), mat)
	ground.SetPosition(glm.Vec3f{0, -60, -400})
	scene.Add(ground)

	cam := scene.NewPerspectiveCamera(60, 1, 1, 5000)
	scene.Add(cam)
	cam.SetPosition(glm.Vec3f{0, 0, 0})
	cam.LookAt(glm.Vec3f{0, 0, -1})

	axis := glm.Vec3f{0.3, 1, 0.2}.Normalize()
	for _, angle := range []float32{0, 0.37, 0.9, 1.4} {
		box.SetRotationQuat(glm.NewQuat(angle, axis))

		// The same frame twice, with nothing advanced in between, so the prepass is the
		// only difference between the two images.
		r.EnableDepthPrepass(false)
		r.Render(scene)
		shaded := append([]byte(nil), r.Pixels()...)

		r.EnableDepthPrepass(true)
		r.Render(scene)
		prepassed := r.Pixels()

		differing := 0
		for i := 0; i < len(shaded); i += 4 {
			if shaded[i] != prepassed[i] || shaded[i+1] != prepassed[i+1] || shaded[i+2] != prepassed[i+2] {
				differing++
			}
		}
		if differing != 0 {
			t.Errorf("box rotated %.2f rad: the prepass changed %d of %d pixels", angle, differing, size*size)
		}
	}
}
