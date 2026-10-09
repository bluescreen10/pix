package pix_test

import (
	"testing"

	"github.com/bluescreen10/pix"
	"github.com/bluescreen10/pix/colors"
	"github.com/bluescreen10/pix/geometries"
	"github.com/bluescreen10/pix/glm"
	"github.com/bluescreen10/pix/materials"
	"github.com/bluescreen10/pix/scenes"
	"github.com/bluescreen10/pix/textures"
)

// Where on a two-texel-wide map each set of twoSetQuad's texture coordinates sits: the
// centre of its left texel, and of its right.
var (
	leftTexel  = glm.Vec2f{0.25, 0.5}
	rightTexel = glm.Vec2f{0.75, 0.5}
)

// twoSetQuad is a quad of the given corners whose first set of texture coordinates sits,
// at every corner, on the centre of the left texel of a two-texel-wide map, and whose
// second set — unless withoutUV2 — on the right's. targets are its morph targets.
func twoSetQuad(corners [4]glm.Vec3f, withoutUV2 bool, targets ...geometries.MorphTarget) geometries.GeometryConfig {
	attributes := []geometries.Attribute{
		geometries.NewAttribute(geometries.AttributePosition, geometries.Float32x3, corners[:]),
		geometries.NewAttribute(geometries.AttributeUV, geometries.Float32x2, []glm.Vec2f{leftTexel, leftTexel, leftTexel, leftTexel}),
	}
	if !withoutUV2 {
		attributes = append(attributes, geometries.NewAttribute(geometries.AttributeUV2, geometries.Float32x2, []glm.Vec2f{rightTexel, rightTexel, rightTexel, rightTexel}))
	}
	return geometries.GeometryConfig{
		Attributes:   attributes,
		Indices:      []uint32{0, 1, 2, 0, 2, 3, 0, 2, 1, 0, 3, 2},
		MorphTargets: targets,
	}
}

// facingCamera is a quad 2 units across facing +z, centred on the origin.
var facingCamera = [4]glm.Vec3f{{-1, -1, 0}, {1, -1, 0}, {1, 1, 0}, {-1, 1, 0}}

// mappedMaterial is a material with a colour map, whose map's set of texture
// coordinates, and the transforms of both sets, can be chosen.
type mappedMaterial interface {
	materials.Material
	SetColorMap(textures.Texture)
	SetColorMapUV(materials.UVSet)
	SetUVTransform(materials.UVSet, materials.UVTransform)
}

// twoSetScene is a renderer and a scene, under a flat white ambient light, with the
// camera looking at the origin down -z.
func twoSetScene(t *testing.T) (*pix.Renderer, *scenes.Scene) {
	t.Helper()
	r, err := pix.NewOffscreenRenderer(32, 32)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(r.Destroy)
	scene := scenes.New()
	t.Cleanup(scene.Destroy)
	scene.SetAmbient(colors.RGB32F{1, 1, 1}, 1)
	cam := scene.NewPerspectiveCamera(45, 1, 0.1, 100)
	cam.SetPosition(glm.Vec3f{0, 0, 3})
	cam.LookAt(glm.Vec3f{})
	return r, scene
}

// redGreenMap is a two-texel map, red on the left and green on the right.
func redGreenMap(t *testing.T, r *pix.Renderer) textures.Texture {
	t.Helper()
	texture := r.TextureStore.Create(nrgbaImage([]byte{255, 0, 0, 255, 0, 255, 0, 255}, 2, 1), textures.SRGB)
	t.Cleanup(texture.Release)
	return texture
}

// centreRedGreen renders the scene and returns the centre pixel's red and green.
func centreRedGreen(r *pix.Renderer, scene *scenes.Scene) (red, green byte) {
	r.Render(scene)
	p := r.Pixels()[(16*32+16)*4:]
	return p[0], p[1]
}

// TestMapsReadTheirUVSet: a colour map reads the set of texture coordinates it is told
// to: the quad's first set, on the red texel, or its second, on the green — for every
// built-in material type.
func TestMapsReadTheirUVSet(t *testing.T) {
	for _, nm := range []struct {
		name string
		new  func(*pix.Renderer) mappedMaterial
	}{
		{"Basic", func(r *pix.Renderer) mappedMaterial { return r.NewBasicMaterial() }},
		{"BlinnPhong", func(r *pix.Renderer) mappedMaterial { return r.NewBlinnPhongMaterial() }},
		{"PBR", func(r *pix.Renderer) mappedMaterial { return r.NewPBRMaterial() }},
	} {
		t.Run(nm.name, func(t *testing.T) {
			r, scene := twoSetScene(t)
			quad := r.GeometryStore.Create(twoSetQuad(facingCamera, false))
			defer quad.Release()
			material := nm.new(r)
			defer material.Release()
			material.SetColorMap(redGreenMap(t, r))
			scene.NewMesh(quad, material)

			if red, green := centreRedGreen(r, scene); red <= green {
				t.Errorf("map reading UV1: centre = red %d green %d, want red", red, green)
			}
			material.SetColorMapUV(materials.UV2)
			if red, green := centreRedGreen(r, scene); green <= red {
				t.Errorf("map reading UV2: centre = red %d green %d, want green", red, green)
			}
		})
	}
}

// TestUVTransformMovesItsSet: a material's transform of one set of texture coordinates
// moves that set alone: moving UV1 onto the green texel leaves a map reading UV2 where
// it was, and moving UV2 onto the red texel moves it.
func TestUVTransformMovesItsSet(t *testing.T) {
	r, scene := twoSetScene(t)
	quad := r.GeometryStore.Create(twoSetQuad(facingCamera, false))
	defer quad.Release()
	material := r.NewBasicMaterial()
	defer material.Release()
	material.SetColorMap(redGreenMap(t, r))
	material.SetColorMapUV(materials.UV2)
	scene.NewMesh(quad, material)

	onto := func(texel glm.Vec2f) materials.UVTransform {
		return materials.UVTransform{Scale: glm.Vec2f{0, 0}, Offset: texel}
	}
	material.SetUVTransform(materials.UV1, onto(rightTexel))
	if red, green := centreRedGreen(r, scene); green <= red {
		t.Errorf("UV1 moved, map reading UV2: centre = red %d green %d, want green", red, green)
	}
	material.SetUVTransform(materials.UV2, onto(leftTexel))
	if red, green := centreRedGreen(r, scene); red <= green {
		t.Errorf("UV2 moved onto the red texel: centre = red %d green %d, want red", red, green)
	}
}

// TestGeometryWithoutUV2ReadsUV1: a map reading UV2 on a geometry that has no second set
// reads its first in its place.
func TestGeometryWithoutUV2ReadsUV1(t *testing.T) {
	r, scene := twoSetScene(t)
	quad := r.GeometryStore.Create(twoSetQuad(facingCamera, true))
	defer quad.Release()
	material := r.NewBasicMaterial()
	defer material.Release()
	material.SetColorMap(redGreenMap(t, r))
	material.SetColorMapUV(materials.UV2)
	scene.NewMesh(quad, material)

	if red, green := centreRedGreen(r, scene); red <= green {
		t.Errorf("map reading UV2 of a geometry without one: centre = red %d green %d, want red, the first set's", red, green)
	}
}

// TestMorphedGeometryKeepsUV2: deforming a geometry carries its second set of texture
// coordinates through to what it draws.
func TestMorphedGeometryKeepsUV2(t *testing.T) {
	r, scene := twoSetScene(t)
	still := geometries.MorphTarget{Name: "still", PositionDeltas: make([]glm.Vec3f, 4)}
	quad := r.GeometryStore.Create(twoSetQuad(facingCamera, false, still))
	defer quad.Release()
	material := r.NewBasicMaterial()
	defer material.Release()
	material.SetColorMap(redGreenMap(t, r))
	material.SetColorMapUV(materials.UV2)
	mesh := scene.NewMesh(quad, material)
	mesh.SetMorphTargetWeight(0, 1)

	if red, green := centreRedGreen(r, scene); green <= red || red > 10 {
		t.Errorf("morphed quad, map reading UV2: centre = red %d green %d, want green", red, green)
	}
}

// TestShadowMaskReadsItsUVSet: the shadow pass cuts a masked plate out by the set of
// texture coordinates its colour map reads: its first set sits on a clear texel, its
// second on a solid one, and the floor under it is lit, or shadowed, as its map reads
// the one or the other.
func TestShadowMaskReadsItsUVSet(t *testing.T) {
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
	m.plate.SetAlphaCutoff(0.5)
	m.plate.SetDoubleSided(true)
	facingUp := [4]glm.Vec3f{{-2, 1, -2}, {2, 1, -2}, {2, 1, 2}, {-2, 1, 2}}
	plate := m.r.GeometryStore.Create(twoSetQuad(facingUp, false))
	defer plate.Release()
	m.scene.NewMesh(plate, m.plate)
	// Looking at the floor under the plate from below its height, so the plate itself
	// is out of view.
	m.cam.SetPosition(glm.Vec3f{0, 0.5, 3})
	m.cam.LookAt(glm.Vec3f{0, 0, 0})

	lit, _ := m.centerColor()
	m.plate.SetColorMapUV(materials.UV2)
	shadowed, _ := m.centerColor()
	if lit <= shadowed+40 {
		t.Errorf("floor under the plate = %d with its map reading the clear UV1, %d reading the solid UV2; want it lit, then shadowed", lit, shadowed)
	}
}

// TestPBRMapsReadTheirUVSet: each of a PBR material's maps reads its own set: its
// emissive map, on UV2, while its colour map is on UV1.
func TestPBRMapsReadTheirUVSet(t *testing.T) {
	r, scene := twoSetScene(t)
	scene.SetAmbient(colors.RGB32F{}, 0)
	quad := r.GeometryStore.Create(twoSetQuad(facingCamera, false))
	defer quad.Release()
	lamp := r.NewPBRMaterial()
	defer lamp.Release()
	lamp.SetColor(colors.RGBA32F{0, 0, 0, 1})
	lamp.SetEmissive(colors.RGB32F{1, 1, 1})
	lamp.SetEmissiveMap(redGreenMap(t, r))
	scene.NewMesh(quad, lamp)

	if red, green := centreRedGreen(r, scene); red <= green {
		t.Errorf("emissive map reading UV1: centre = red %d green %d, want red", red, green)
	}
	lamp.SetEmissiveMapUV(materials.UV2)
	if red, green := centreRedGreen(r, scene); green <= red {
		t.Errorf("emissive map reading UV2: centre = red %d green %d, want green", red, green)
	}
}
