package pix_test

import (
	"testing"

	"github.com/bluescreen10/pix"
	"github.com/bluescreen10/pix/colors"
	"github.com/bluescreen10/pix/geometries"
	"github.com/bluescreen10/pix/glm"
	"github.com/bluescreen10/pix/scenes"
)

// slopeQuad is one quad with corners and a normal shared by all four.
func slopeQuad(corners [4]glm.Vec3f, normal glm.Vec3f) geometries.GeometryConfig {
	positions := corners[:]
	normals := []glm.Vec3f{normal, normal, normal, normal}
	return geometries.GeometryConfig{
		Attributes: []geometries.Attribute{
			geometries.NewAttribute(geometries.AttributePosition, geometries.Float32x3, positions),
			geometries.NewAttribute(geometries.AttributeNormal, geometries.Float32x3, normals),
		},
		Indices: []uint32{0, 1, 2, 0, 2, 3},
	}
}

// shadeSlope renders quad, scaled by scale, lit from straight above, and returns the
// centre pixel.
func shadeSlope(t *testing.T, quad geometries.GeometryConfig, scale glm.Vec3f) [3]byte {
	t.Helper()
	const size = 64
	r, err := pix.NewOffscreenRenderer(size, size)
	if err != nil {
		t.Fatal(err)
	}
	defer r.Destroy()
	scene := scenes.New()
	defer scene.Destroy()
	scene.SetAmbient(colors.RGB32F{0, 0, 0}, 0)
	scene.AddDirectionalLight(glm.Vec3f{0, -1, 0}, colors.RGB32F{1, 1, 1}, 1)

	geometry := r.GeometryStore.Create(quad)
	defer geometry.Release()
	material := r.NewPBRMaterial()
	defer material.Release()
	material.SetColor(colors.RGBA32F{1, 1, 1, 1})
	material.SetRoughness(1)
	mesh := scene.NewMesh(geometry, material)
	mesh.SetScale(scale)
	scene.Add(mesh)

	cam := scene.NewPerspectiveCamera(45, 1, 0.1, 100)
	scene.Add(cam)
	cam.SetPosition(glm.Vec3f{0, 6, 6})
	cam.LookAt(glm.Vec3f{0, 0, 0})

	r.Render(scene)
	pixels := r.Pixels()
	i := (size/2*size + size/2) * 4
	return [3]byte{pixels[i], pixels[i+1], pixels[i+2]}
}

// TestNormalsFollowNonUniformScale: a surface stretched by its transform turns its
// normal as the surface turns — by the transform's inverse transpose, not the transform
// itself. A 45° slope stretched four times along z lies almost flat, and must shade like
// the same flat-lying slope built in place; turning its normal by the transform instead
// stands it almost upright, and the light from above barely reaches it.
func TestNormalsFollowNonUniformScale(t *testing.T) {
	// The slope rises along -z: its normal is (0, 1, 1) normalized.
	slope := slopeQuad([4]glm.Vec3f{{-1, -0.5, 0.5}, {1, -0.5, 0.5}, {1, 0.5, -0.5}, {-1, 0.5, -0.5}},
		glm.Vec3f{0, 1, 1}.Normalize())
	// The same slope stretched four times along z, built so; its normal is (0, 4, 1)
	// normalized, perpendicular to the stretched rise (0, 1, -4).
	stretched := slopeQuad([4]glm.Vec3f{{-1, -0.5, 2}, {1, -0.5, 2}, {1, 0.5, -2}, {-1, 0.5, -2}},
		glm.Vec3f{0, 4, 1}.Normalize())

	got := shadeSlope(t, slope, glm.Vec3f{1, 1, 4})
	want := shadeSlope(t, stretched, glm.Vec3f{1, 1, 1})
	if want[0] < 64 {
		t.Fatalf("the slope built stretched shades %v, too dark to compare against", want)
	}
	for c := range got {
		if diff := int(got[c]) - int(want[c]); diff < -3 || diff > 3 {
			t.Fatalf("slope stretched by its transform shades %v, want %v like the slope built stretched", got, want)
		}
	}
}
