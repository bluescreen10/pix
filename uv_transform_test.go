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

// texelTransform collapses every texture coordinate onto the centre of one texel of a
// two-texel-wide map: the left one, or the right. Where on the mesh that texel would
// otherwise show does not matter, so the tests below need not know how a mesh lays out
// its coordinates.
func texelTransform(right bool) materials.UVTransform {
	u := float32(0.25)
	if right {
		u = 0.75
	}
	return materials.UVTransform{Scale: glm.Vec2f{0, 0}, Offset: glm.Vec2f{u, 0.5}}
}

// TestUVTransformMovesTheColorMap: a plate whose colour map is red on the left and green
// on the right shows red everywhere moved onto the left texel, and green moved onto the
// right — for every built-in material type.
func TestUVTransformMovesTheColorMap(t *testing.T) {
	type transformedMaterial interface {
		materials.Material
		SetColorMap(textures.Texture)
		SetUVTransform(materials.UVSet, materials.UVTransform)
	}
	newMaterials := []struct {
		name string
		new  func(*pix.Renderer) transformedMaterial
	}{
		{"Basic", func(r *pix.Renderer) transformedMaterial { return r.NewBasicMaterial() }},
		{"BlinnPhong", func(r *pix.Renderer) transformedMaterial { return r.NewBlinnPhongMaterial() }},
		{"PBR", func(r *pix.Renderer) transformedMaterial { return r.NewPBRMaterial() }},
	}
	for _, nm := range newMaterials {
		t.Run(nm.name, func(t *testing.T) {
			r, err := pix.NewOffscreenRenderer(32, 32)
			if err != nil {
				t.Fatal(err)
			}
			defer r.Destroy()
			scene := scenes.New()
			defer scene.Destroy()
			scene.SetAmbient(colors.RGB32F{1, 1, 1}, 1)

			redGreen := r.TextureStore.Create(nrgbaImage([]byte{255, 0, 0, 255, 0, 255, 0, 255}, 2, 1), textures.SRGB)
			defer redGreen.Release()
			material := nm.new(r)
			defer material.Release()
			material.SetColorMap(redGreen)
			scene.NewMesh(r.NewBoxGeometry(2, 2, 0.05), material)
			cam := scene.NewPerspectiveCamera(45, 1, 0.1, 100)
			cam.SetPosition(glm.Vec3f{0, 0, 3})
			cam.LookAt(glm.Vec3f{})

			for _, right := range []bool{false, true} {
				material.SetUVTransform(materials.UV1, texelTransform(right))
				r.Render(scene)
				p := r.Pixels()[(16*32+16)*4:]
				red, green := p[0], p[1]
				if !right && red <= green {
					t.Errorf("moved onto the red texel: centre = red %d green %d, want red", red, green)
				}
				if right && green <= red {
					t.Errorf("moved onto the green texel: centre = red %d green %d, want green", red, green)
				}
			}
		})
	}
}

// TestUVTransformMovesTheShadowMask: the shadow pass cuts a masked plate out through its
// UV transform, as its own shader does. The plate's colour map is clear on the left and
// solid on the right; untransformed, the middle of the plate blends the two, which is
// above the cut-off, so a shadow pass that ignored the transform would still shadow the
// floor beneath it.
func TestUVTransformMovesTheShadowMask(t *testing.T) {
	m := newMaskedPlate(t)
	m.r.EnableShadows(true)
	m.scene.SetAmbient(colors.RGB32F{}, 0)
	sun := m.scene.AddDirectionalLight(glm.Vec3f{0, -1, 0}, colors.RGB32F{1, 1, 1}, 2)
	sun.SetCastShadow(true)
	floor := m.r.NewPBRMaterial()
	floor.SetMetallic(0)
	floor.SetRoughness(1)
	m.scene.NewMesh(m.r.NewPlaneGeometry(20, 20, 1, 1), floor)

	clearSolid := m.r.TextureStore.Create(nrgbaImage([]byte{255, 255, 255, 0, 255, 255, 255, 255}, 2, 1), textures.SRGB)
	defer clearSolid.Release()
	m.plate.SetColorMap(clearSolid)
	m.plate.SetAlphaCutoff(0.3)
	plateMesh := m.scene.NewMesh(m.r.NewBoxGeometry(4, 0.05, 4), m.plate)
	plateMesh.SetPosition(glm.Vec3f{0, 1, 0})
	// Looking at the floor under the plate from below its height, so the plate itself
	// is out of view.
	m.cam.SetPosition(glm.Vec3f{0, 0.5, 3})
	m.cam.LookAt(glm.Vec3f{0, 0, 0})

	m.plate.SetUVTransform(materials.UV1, texelTransform(true))
	shadowed, _ := m.centerColor()
	m.plate.SetUVTransform(materials.UV1, texelTransform(false))
	lit, _ := m.centerColor()
	if lit <= shadowed+40 {
		t.Errorf("floor under the plate = %d moved onto its clear texel, %d onto its solid one; want it lit once clear", lit, shadowed)
	}
}

// TestUVTransformDrawsBesideUntransformed: materials of one pool that move their
// coordinates and that do not are drawn through pipelines of their own, in one frame —
// the untransformed one's skips the transform — and a material given a transform after
// it was first drawn moves to the transformed one. Both plates' coordinates sit on the
// red texel; a transform moves one onto the green.
func TestUVTransformDrawsBesideUntransformed(t *testing.T) {
	r, scene := twoSetScene(t)
	scene.SetAmbient(colors.RGB32F{1, 1, 1}, 1)
	redGreen := redGreenMap(t, r)
	newPlate := func(x float32) *materials.BasicMaterial {
		corners := [4]glm.Vec3f{{x - 0.5, -0.5, 0}, {x + 0.5, -0.5, 0}, {x + 0.5, 0.5, 0}, {x - 0.5, 0.5, 0}}
		quad := r.GeometryStore.Create(twoSetQuad(corners, true))
		t.Cleanup(quad.Release)
		m := r.NewBasicMaterial()
		t.Cleanup(m.Release)
		m.SetColorMap(redGreen)
		scene.NewMesh(quad, m)
		return m
	}
	moved, still := newPlate(-0.6), newPlate(0.6)
	moved.SetUVTransform(materials.UV1, texelTransform(true))

	// The plates sit left and right of the frame's middle, at pixels 8 and 23 of the
	// middle row; which is which on screen is the camera's business.
	plateColors := func() (left, right [2]byte) {
		r.Render(scene)
		px := r.Pixels()
		at := func(x int) [2]byte {
			i := (16*32 + x) * 4
			return [2]byte{px[i], px[i+1]}
		}
		return at(8), at(23)
	}
	isGreen := func(c [2]byte) bool {
		return c[1] > c[0]
	}

	left, right := plateColors()
	if isGreen(left) == isGreen(right) {
		t.Errorf("transformed and untransformed plate = red/green %v and %v, want one green and one red", left, right)
	}
	still.SetUVTransform(materials.UV1, texelTransform(true))
	if left, right := plateColors(); !isGreen(left) || !isGreen(right) {
		t.Errorf("both plates transformed = red/green %v and %v, want both green", left, right)
	}
}
