package pix_test

import (
	"image"
	"testing"

	"github.com/bluescreen10/pix"
	"github.com/bluescreen10/pix/colors"
	"github.com/bluescreen10/pix/geometries"
	"github.com/bluescreen10/pix/glm"
	"github.com/bluescreen10/pix/materials"
	"github.com/bluescreen10/pix/scenes"
	"github.com/bluescreen10/pix/textures"
)

// TestTransparency renders an opaque red quad behind a 50%-alpha blue quad in front.
// The overlap must blend (both channels present) — proving alpha blending, the
// opaque-before-transparent ordering, and depth-test-but-no-depth-write.
func TestTransparency(t *testing.T) {
	const size = 96
	r, err := pix.NewOffscreenRenderer(size, size)
	if err != nil {
		t.Fatal(err)
	}
	defer r.Destroy()
	r.SetClearColor([4]float32{0, 0, 0, 1})
	scene := scenes.New()
	defer scene.Destroy()
	scene.SetAmbient(colors.RGB32F{1, 1, 1}, 1) // full ambient → albedo shows directly

	quad := func(z float32) geometries.Geometry {
		return r.GeometryStore.Create(geometries.GeometryConfig{
			Attributes: []geometries.Attribute{
				geometries.NewAttribute(geometries.AttributePosition, geometries.Float32x3, []glm.Vec3f{{-0.8, -0.8, z}, {0.8, -0.8, z}, {0.8, 0.8, z}, {-0.8, 0.8, z}}),
			},
			Indices: []uint32{0, 1, 2, 0, 2, 3},
		})
	}

	// Opaque red behind. Create it LAST to prove the sort still draws opaque first.
	red := r.NewPBRMaterial()
	red.SetColor(colors.RGBA32F{1, 0, 0, 1})
	// Transparent blue in front (created first).
	blue := r.NewPBRMaterial()
	blue.SetColor(colors.RGBA32F{0, 0, 1, 0.5})
	blue.SetBlend(materials.BlendAlpha)
	scene.NewMesh(quad(0), blue)   // front, transparent, created first
	scene.NewMesh(quad(-0.5), red) // behind, opaque, created last

	cam := scene.NewPerspectiveCamera(45, 1, 0.1, 1000)
	cam.SetPosition(glm.Vec3f{0, 0, 2})
	r.Render(scene)

	px := r.Pixels()
	i := (size/2*size + size/2) * 4 // center (overlap)
	rr, gg, bb := px[i], px[i+1], px[i+2]
	t.Logf("center pixel = (%d,%d,%d)", rr, gg, bb)
	if rr < 40 || bb < 40 {
		t.Fatalf("no blend at overlap: (%d,%d,%d) — red should show through the blue", rr, gg, bb)
	}
}

// TestBlendChangeRebatches: turning a material transparent after it has already been
// drawn must re-batch the scene, not keep drawing it through the opaque pipeline it
// resolved to the first time.
//
// The mesh table does not move when this happens — the same objects are drawn with the
// same materials — so the scene's own revision cannot report it. What reports it is the
// material pool's rasterization revision, which blend and cull are the whole of (see
// materials.Pool.RasterRevision and Renderer.isLayoutStale).
func TestBlendChangeRebatches(t *testing.T) {
	const size = 96
	r, err := pix.NewOffscreenRenderer(size, size)
	if err != nil {
		t.Fatal(err)
	}
	defer r.Destroy()
	r.SetClearColor([4]float32{0, 0, 0, 1})
	scene := scenes.New()
	defer scene.Destroy()
	scene.SetAmbient(colors.RGB32F{1, 1, 1}, 1) // full ambient → albedo shows directly

	quad := func(z float32) geometries.Geometry {
		return r.GeometryStore.Create(geometries.GeometryConfig{
			Attributes: []geometries.Attribute{
				geometries.NewAttribute(geometries.AttributePosition, geometries.Float32x3, []glm.Vec3f{{-0.8, -0.8, z}, {0.8, -0.8, z}, {0.8, 0.8, z}, {-0.8, 0.8, z}}),
			},
			Indices: []uint32{0, 1, 2, 0, 2, 3},
		})
	}

	red := r.NewPBRMaterial()
	red.SetColor(colors.RGBA32F{1, 0, 0, 1})
	blue := r.NewPBRMaterial()
	blue.SetColor(colors.RGBA32F{0, 0, 1, 0.5}) // alpha set now, but still opaque-blended
	scene.NewMesh(quad(-0.5), red)
	scene.NewMesh(quad(0), blue)

	cam := scene.NewPerspectiveCamera(45, 1, 0.1, 1000)
	cam.SetPosition(glm.Vec3f{0, 0, 2})
	i := (size/2*size + size/2) * 4

	// Opaque to begin with: the blue quad in front hides the red one entirely.
	r.Render(scene)
	if px := r.Pixels(); px[i] > 40 || px[i+2] < 200 {
		t.Fatalf("before the change: center = (%d,%d,%d), want opaque blue", px[i], px[i+1], px[i+2])
	}

	blue.SetBlend(materials.BlendAlpha)
	r.Render(scene)
	px := r.Pixels()
	if px[i] < 40 || px[i+2] < 40 {
		t.Fatalf("after SetBlend: center = (%d,%d,%d), want red showing through blue — "+
			"the draw list kept the opaque pipeline it batched with", px[i], px[i+1], px[i+2])
	}
}

// TestTransparentBehindOpaqueIsHidden renders a 50%-alpha blue quad behind an opaque
// red one. Blended surfaces are drawn in a render pass of their own, after the opaque
// ones; that pass must test against the depth the opaque pass left, or the blue quad
// blends over the red one that hides it.
func TestTransparentBehindOpaqueIsHidden(t *testing.T) {
	const size = 96
	r, err := pix.NewOffscreenRenderer(size, size)
	if err != nil {
		t.Fatal(err)
	}
	defer r.Destroy()
	r.SetClearColor([4]float32{0, 0, 0, 1})
	scene := scenes.New()
	defer scene.Destroy()
	scene.SetAmbient(colors.RGB32F{1, 1, 1}, 1) // full ambient → albedo shows directly

	quad := func(z float32) geometries.Geometry {
		return r.GeometryStore.Create(geometries.GeometryConfig{
			Attributes: []geometries.Attribute{
				geometries.NewAttribute(geometries.AttributePosition, geometries.Float32x3, []glm.Vec3f{{-0.8, -0.8, z}, {0.8, -0.8, z}, {0.8, 0.8, z}, {-0.8, 0.8, z}}),
			},
			Indices: []uint32{0, 1, 2, 0, 2, 3},
		})
	}

	red := r.NewPBRMaterial()
	red.SetColor(colors.RGBA32F{1, 0, 0, 1})
	blue := r.NewPBRMaterial()
	blue.SetColor(colors.RGBA32F{0, 0, 1, 0.5})
	blue.SetBlend(materials.BlendAlpha)
	scene.NewMesh(quad(0), red)     // front, opaque
	scene.NewMesh(quad(-0.5), blue) // behind, transparent

	cam := scene.NewPerspectiveCamera(45, 1, 0.1, 1000)
	cam.SetPosition(glm.Vec3f{0, 0, 2})
	r.Render(scene)

	px := r.Pixels()
	i := (size/2*size + size/2) * 4 // center (overlap)
	if px[i] < 200 || px[i+1] != 0 || px[i+2] != 0 {
		t.Fatalf("center = (%d,%d,%d), want opaque red with no blue blended over it", px[i], px[i+1], px[i+2])
	}
}

// TestBlendedMeshesDrawBackToFront renders a 50%-alpha red quad and a 50%-alpha blue
// one, overlapping over black, from in front and then from behind. Blending composites
// each surface over what is already drawn, so the nearer quad has to be drawn last: the
// overlap is half the nearer colour plus a quarter of the farther, (0.5, 0.25) linear,
// which the display target encodes as 188 and 137.
//
// Both quads share a geometry and a material pool, which is what used to make them one
// batch drawn in whatever order the cull kept them; and moving the camera changes no
// layout, so only a sort redone every frame gets both views right.
func TestBlendedMeshesDrawBackToFront(t *testing.T) {
	const size = 96
	r, err := pix.NewOffscreenRenderer(size, size)
	if err != nil {
		t.Fatal(err)
	}
	defer r.Destroy()
	r.SetClearColor([4]float32{0, 0, 0, 1})
	scene := scenes.New()
	defer scene.Destroy()

	quad := r.GeometryStore.Create(geometries.GeometryConfig{
		Attributes: []geometries.Attribute{
			geometries.NewAttribute(geometries.AttributePosition, geometries.Float32x3, []glm.Vec3f{{-0.8, -0.8, 0}, {0.8, -0.8, 0}, {0.8, 0.8, 0}, {-0.8, 0.8, 0}}),
		},
		Indices: []uint32{0, 1, 2, 0, 2, 3},
	})
	pane := func(color colors.RGBA32F, z float32) {
		material := r.NewBasicMaterial()
		material.SetColor(color)
		material.SetBlend(materials.BlendAlpha)
		material.SetDoubleSided(true)
		mesh := scene.NewMesh(quad, material)
		mesh.SetPosition(glm.Vec3f{0, 0, z})
	}
	pane(colors.RGBA32F{1, 0, 0, 0.5}, 0)
	pane(colors.RGBA32F{0, 0, 1, 0.5}, -0.5)

	cam := scene.NewPerspectiveCamera(45, 1, 0.1, 100)
	for _, view := range []struct {
		z    float32
		want [3]byte
		name string
	}{
		{2, [3]byte{188, 0, 137}, "from in front, red is nearer"},
		{-2.5, [3]byte{137, 0, 188}, "from behind, blue is nearer"},
	} {
		cam.SetPosition(glm.Vec3f{0, 0, view.z})
		cam.LookAt(glm.Vec3f{0, 0, -0.25})
		r.Render(scene)

		if got := pixelAt(r, size/2, size/2); got != view.want {
			t.Errorf("%s: center = %v, want %v", view.name, got, view.want)
		}
	}
}

// TestGlassReflectsInFull renders a highlight on a pane of glass over a red backdrop,
// and the same highlight on an opaque surface of the same finish with nothing to
// show but it. Glass lets almost everything behind it through face-on, but that does
// not dim what it reflects: the highlight must come out as bright as on the opaque
// surface, with the red behind showing through it.
//
// Under plain alpha blending it does not. The glass's alpha is how much of the
// backdrop it keeps out, about 0.04 face-on, and that same alpha scales the colour it
// reflects, so the highlight all but disappears.
func TestGlassReflectsInFull(t *testing.T) {
	const size = 96
	r, err := pix.NewOffscreenRenderer(size, size)
	if err != nil {
		t.Fatal(err)
	}
	defer r.Destroy()
	r.SetClearColor([4]float32{0, 0, 0, 1})

	quad := func(z float32) geometries.Geometry {
		return r.GeometryStore.Create(geometries.GeometryConfig{
			Attributes: []geometries.Attribute{
				geometries.NewAttribute(geometries.AttributePosition, geometries.Float32x3, []glm.Vec3f{{-0.8, -0.8, z}, {0.8, -0.8, z}, {0.8, 0.8, z}, {-0.8, 0.8, z}}),
				geometries.NewAttribute(geometries.AttributeNormal, geometries.Float32x3, []glm.Vec3f{{0, 0, 1}, {0, 0, 1}, {0, 0, 1}, {0, 0, 1}}),
			},
			Indices: []uint32{0, 1, 2, 0, 2, 3},
		})
	}
	// renderCenter draws surface in front of the camera, lit head-on so that its
	// highlight lands on the centre of the frame, and returns the centre pixel.
	renderCenter := func(surface, backdrop materials.Material) [3]uint8 {
		scene := scenes.New()
		defer scene.Destroy()
		scene.AddDirectionalLight(glm.Vec3f{0, 0, -1}, colors.RGB32F{1, 1, 1}, 3)
		scene.NewMesh(quad(0), surface)
		if backdrop != nil {
			scene.NewMesh(quad(-0.5), backdrop)
		}
		cam := scene.NewPerspectiveCamera(45, 1, 0.1, 1000)
		cam.SetPosition(glm.Vec3f{0, 0, 2})
		r.Render(scene)
		px := r.Pixels()
		i := (size/2*size + size/2) * 4
		return [3]uint8{px[i], px[i+1], px[i+2]}
	}

	// A black dielectric reflects its highlight and nothing else.
	opaque := r.NewPBRMaterial()
	opaque.SetColor(colors.RGBA32F{0, 0, 0, 1})
	opaque.SetMetallic(0)
	opaque.SetRoughness(0.4)
	highlight := renderCenter(opaque, nil)
	if highlight[1] < 40 {
		t.Fatalf("opaque highlight = %v, too dim to compare against", highlight)
	}

	glass := r.NewPBRMaterial()
	glass.SetColor(colors.RGBA32F{1, 1, 1, 1})
	glass.SetMetallic(0)
	glass.SetRoughness(0.4)
	glass.SetTransmission(1)
	// With a thickness, one surface, like the opaque one: a pane with none reflects from
	// both its surfaces, and twice the highlight (see TestThinGlassReflectsFromBothSurfaces).
	glass.SetThickness(0.1)
	// Unlit, so that the backdrop has no highlight of its own to show through.
	red := r.NewBasicMaterial()
	red.SetColor(colors.RGBA32F{1, 0, 0, 1})
	got := renderCenter(glass, red)

	// The backdrop has no green or blue, so those channels are the highlight alone.
	if diff := int(got[1]) - int(highlight[1]); diff < -3 || diff > 3 {
		t.Errorf("glass highlight green = %d, want %d (the opaque surface's)", got[1], highlight[1])
	}
	if diff := int(got[2]) - int(highlight[2]); diff < -3 || diff > 3 {
		t.Errorf("glass highlight blue = %d, want %d (the opaque surface's)", got[2], highlight[2])
	}
	if got[0] < 200 {
		t.Errorf("glass red = %d, want the red backdrop showing through (>= 200)", got[0])
	}
}

// glassShadowScene is a white floor under a sun straight above, and a camera low
// enough to look at the floor where a plate would be held over it without seeing the
// plate.
type glassShadowScene struct {
	r     *pix.Renderer
	scene *scenes.Scene
}

func newGlassShadowScene(t *testing.T) glassShadowScene {
	t.Helper()
	r, err := pix.NewOffscreenRenderer(32, 32)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(r.Destroy)
	r.EnableShadows(true)
	scene := scenes.New()
	t.Cleanup(scene.Destroy)
	scene.SetAmbient(colors.RGB32F{}, 0)
	sun := scene.AddDirectionalLight(glm.Vec3f{0, -1, 0}, colors.RGB32F{1, 1, 1}, 2)
	sun.SetCastShadow(true)

	floor := r.NewPBRMaterial()
	floor.SetMetallic(0)
	floor.SetRoughness(1)
	scene.NewMesh(r.NewPlaneGeometry(20, 20, 1, 1), floor)
	cam := scene.NewPerspectiveCamera(45, 1, 0.1, 100)
	cam.SetPosition(glm.Vec3f{0, 0.5, 3})
	cam.LookAt(glm.Vec3f{0, 0, 0})
	return glassShadowScene{r: r, scene: scene}
}

// addPlate holds a thin plate of material over the floor.
func (g glassShadowScene) addPlate(material materials.Material) {
	plate := g.scene.NewMesh(g.r.NewBoxGeometry(4, 0.05, 4), material)
	plate.SetPosition(glm.Vec3f{0, 1, 0})
}

// floorUnderPlate renders and returns the red of the floor under the plate.
func (g glassShadowScene) floorUnderPlate() int {
	for range 3 {
		g.r.Render(g.scene)
	}
	return int(g.r.Pixels()[(16*32+16)*4])
}

// newGlass is a clear pane: transmission 1, smooth, not metal.
func newGlass(r *pix.Renderer) *materials.PBRMaterial {
	glass := r.NewPBRMaterial()
	glass.SetColor(colors.RGBA32F{1, 1, 1, 1})
	glass.SetMetallic(0)
	glass.SetRoughness(0)
	glass.SetTransmission(1)
	return glass
}

// TestGlassCastsNoShadow holds a pane of glass over a floor in the sun: the floor under
// it is as bright as with nothing there, where an opaque plate shadows it.
func TestGlassCastsNoShadow(t *testing.T) {
	open := newGlassShadowScene(t).floorUnderPlate()

	opaque := newGlassShadowScene(t)
	plate := opaque.r.NewPBRMaterial()
	plate.SetMetallic(0)
	opaque.addPlate(plate)
	shadowed := opaque.floorUnderPlate()
	if shadowed > open-40 {
		t.Fatalf("floor under an opaque plate = %d, open floor = %d: no shadow to compare against", shadowed, open)
	}

	glassy := newGlassShadowScene(t)
	glassy.addPlate(newGlass(glassy.r))
	if got := glassy.floorUnderPlate(); absDiff(got, open) > 3 {
		t.Errorf("floor under glass = %d, want %d, as with nothing over it (an opaque plate leaves %d)", got, open, shadowed)
	}
}

// TestGlassShadowFollowsItsTransmissionMap: a pane whose transmission map says it lets
// no light through casts a shadow like any opaque plate, and one whose map says it lets
// it all through casts none — a cabinet with glass panes shadows the floor under its
// wood and not under its glass.
func TestGlassShadowFollowsItsTransmissionMap(t *testing.T) {
	floorUnder := func(mapValue byte) int {
		g := newGlassShadowScene(t)
		glass := newGlass(g.r)
		transmissionMap := g.r.TextureStore.Create(&image.Gray{Pix: []byte{mapValue}, Stride: 1, Rect: image.Rect(0, 0, 1, 1)}, textures.Grayscale)
		defer transmissionMap.Release()
		glass.SetTransmissionMap(transmissionMap)
		glass.SetTransmissionMapSampler(g.r.TextureStore.DefaultSampler())
		g.addPlate(glass)
		return g.floorUnderPlate()
	}
	clear, opaque := floorUnder(255), floorUnder(0)
	if clear <= opaque+40 {
		t.Errorf("floor under glass mapped clear = %d, mapped opaque = %d; want it lit under the clear one, shadowed under the other", clear, opaque)
	}
}

// TestDoubleSidedBlendedMeshDrawsItsBackFirst renders one double-sided, 50%-alpha mesh
// of two walls over black: a red one facing the camera, and behind it a blue one facing
// away, as a jar's far wall does. The red wall comes first in the mesh's triangles. The
// far wall has to be blended first all the same, so the overlap is half red plus a
// quarter of blue, (0.5, 0, 0.25) linear, which the display target encodes as 188 and
// 137; drawn in triangle order, red and blue swap.
func TestDoubleSidedBlendedMeshDrawsItsBackFirst(t *testing.T) {
	const size = 64
	r, err := pix.NewOffscreenRenderer(size, size)
	if err != nil {
		t.Fatal(err)
	}
	defer r.Destroy()
	r.SetClearColor(colors.RGBA32F{0, 0, 0, 1})
	scene := scenes.New()
	defer scene.Destroy()

	red, blue := glm.Vec4f{1, 0, 0, 1}, glm.Vec4f{0, 0, 1, 1}
	walls := r.GeometryStore.Create(geometries.GeometryConfig{
		Attributes: []geometries.Attribute{
			geometries.NewAttribute(geometries.AttributePosition, geometries.Float32x3, []glm.Vec3f{
				{-0.8, -0.8, 0}, {0.8, -0.8, 0}, {0.8, 0.8, 0}, {-0.8, 0.8, 0},
				{-0.8, -0.8, -0.5}, {0.8, -0.8, -0.5}, {0.8, 0.8, -0.5}, {-0.8, 0.8, -0.5},
			}),
			geometries.NewAttribute(geometries.AttributeColor, geometries.Float32x4, []glm.Vec4f{
				red, red, red, red, blue, blue, blue, blue,
			}),
		},
		// The near wall counter-clockwise from the camera, facing it; the far wall
		// clockwise, facing away.
		Indices: []uint32{0, 1, 2, 0, 2, 3, 4, 6, 5, 4, 7, 6},
	})
	material := r.NewBasicMaterial()
	material.SetColor(colors.RGBA32F{1, 1, 1, 0.5})
	material.SetBlend(materials.BlendAlpha)
	material.SetDoubleSided(true)
	scene.NewMesh(walls, material)

	cam := scene.NewPerspectiveCamera(45, 1, 0.1, 100)
	cam.SetPosition(glm.Vec3f{0, 0, 2})
	cam.LookAt(glm.Vec3f{})
	r.Render(scene)

	px := r.Pixels()
	i := (size/2*size + size/2) * 4
	if got := [3]byte{px[i], px[i+1], px[i+2]}; absDiff(int(got[0]), 188) > 3 || absDiff(int(got[2]), 137) > 3 {
		t.Errorf("overlap = %v, want (188, 0, 137): the far blue wall blended first, the near red one over it", got)
	}
}
