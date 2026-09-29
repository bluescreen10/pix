package pix_test

import (
	"testing"

	"github.com/bluescreen10/pix"
	"github.com/bluescreen10/pix/colors"
	"github.com/bluescreen10/pix/glm"
	"github.com/bluescreen10/pix/scenes"
)

// occluderScene is a ground slab with a cube floating over it, lit by a shadow-casting
// directional light — the smallest scene where a directional shadow is visible. It
// returns the light's id too, so a test can ask the renderer for the fitted camera.
//
// origin places the whole arrangement, camera included, somewhere in the world. Only the
// far-from-origin test passes anything but the zero vector; a shadow fit must not care
// where in the world it is working, and one of these algorithms used to.
//
// The caller owns the scene and must defer its Destroy, so that it runs before the
// renderer's: the scene holds geometry and material references into the renderer's
// stores, and releasing them after those stores are gone panics.
func occluderScene(t *testing.T, r *pix.Renderer, origin glm.Vec3f) (*scenes.Scene, scenes.LightID) {
	t.Helper()
	scene := scenes.New()
	scene.SetAmbient(colors.RGB32F{0.05, 0.05, 0.05})
	light := scene.AddDirectionalLight(glm.Vec3f{0.15, -1, 0.15}, colors.RGB32F{1, 1, 1}, 3)
	light.SetCastShadow(true)

	cube := r.GeometryStore.Create(pix.BoxGeometry(1, 1, 1))
	defer cube.Release() // every mesh below takes its own reference

	ground := scene.NewMesh(cube, r.NewPBRMaterial())
	ground.SetPosition(origin)
	ground.SetScale(glm.Vec3f{6, 0.2, 6})
	scene.Add(ground)

	occluder := scene.NewMesh(cube, r.NewPBRMaterial())
	occluder.SetPosition(glm.Vec3f{0, 1.5, 0}.Add(origin))
	occluder.SetScale(glm.Vec3f{0.8, 0.8, 0.8})
	occluder.SetCastShadow(true)
	scene.Add(occluder)

	cam := scene.NewPerspectiveCamera(45, 1, 0.1, 100)
	scene.Add(cam)
	cam.SetPosition(glm.Vec3f{0, 5, 6}.Add(origin))
	cam.LookAt(origin)
	return scene, light.ID()
}

// TestShadowSettingsLevels: the number of shadow cameras a fit needs is what decides
// how wide a map to allocate, so every settings value has to report it — including the
// zero values, which is what makes ShadowCascaded{} usable without filling anything in.
func TestShadowSettingsLevels(t *testing.T) {
	r, err := pix.NewOffscreenRenderer(32, 32)
	if err != nil {
		t.Fatal(err)
	}
	defer r.Destroy()

	r.SetShadows(pix.ShadowUniform{})
	if n := len(fitCascades(t, r)); n != 0 {
		t.Errorf("the uniform fit produced %d cascades; it is a single camera", n)
	}
	r.SetShadows(pix.ShadowCascaded{})
	if n := len(fitCascades(t, r)); n != pix.DefaultShadowCascades {
		t.Errorf("a zero ShadowCascaded produced %d slices, want the default %d",
			n, pix.DefaultShadowCascades)
	}
	r.SetShadows(pix.ShadowCascaded{Levels: 99})
	if n := len(fitCascades(t, r)); n != pix.MaxShadowCascades {
		t.Errorf("Levels 99 produced %d slices, want it clamped to %d", n, pix.MaxShadowCascades)
	}
	// Steps decide the count when they are used, so Levels does not get a say.
	r.SetShadows(pix.ShadowCascaded{Levels: 4, Steps: []float32{5, 30}})
	if n := len(fitCascades(t, r)); n != 2 {
		t.Errorf("two steps with Levels 4 produced %d slices, want 2", n)
	}
}

// fitCascades renders a scene and reports the cascades the renderer's current settings
// produced.
func fitCascades(t *testing.T, r *pix.Renderer) []float32 {
	t.Helper()
	r.EnableShadows(true)
	scene, id := occluderScene(t, r, glm.Vec3f{})
	defer scene.Destroy()
	r.Render(scene)
	return r.ShadowView(scene.ID(), id).Splits
}

// TestShadowSettingsDefaultToUniform: cascades cost a depth pass and a wider map per
// slice, so they have to be opted into — a renderer nobody configured keeps the single
// fit.
func TestShadowSettingsDefaultToUniform(t *testing.T) {
	r, err := pix.NewOffscreenRenderer(32, 32)
	if err != nil {
		t.Fatal(err)
	}
	defer r.Destroy()

	if _, ok := r.Shadows().(pix.ShadowUniform); !ok {
		t.Fatalf("default settings = %T, want ShadowUniform", r.Shadows())
	}
	r.SetShadows(pix.ShadowCascaded{Levels: 3})
	c, ok := r.Shadows().(pix.ShadowCascaded)
	if !ok {
		t.Fatalf("after SetShadows(cascaded): %T", r.Shadows())
	}
	if c.Levels != 3 {
		t.Errorf("the settings came back with Levels %d, want 3", c.Levels)
	}
}

// TestShadowAlgorithmSwitchesAtRuntime: one algorithm fits a single camera and the
// other fits several into a wider map, so flipping between them has to rebuild what a
// light is holding rather than render from whichever of the two ran last.
func TestShadowAlgorithmSwitchesAtRuntime(t *testing.T) {
	r, err := pix.NewOffscreenRenderer(160, 160)
	if err != nil {
		t.Fatal(err)
	}
	defer r.Destroy()
	r.EnableShadows(true)
	r.SetClearColor(colors.RGBA32F{0, 0, 0, 1})

	scene, id := occluderScene(t, r, glm.Vec3f{})
	defer scene.Destroy()

	luma := func() int64 {
		r.Render(scene)
		px := r.Pixels()
		var sum int64
		for i := 0; i+3 < len(px); i += 4 {
			sum += int64(px[i]) + int64(px[i+1]) + int64(px[i+2])
		}
		return sum
	}
	slices := func() int {
		return len(r.ShadowView(scene.ID(), id).Cascades)
	}

	r.SetShadows(pix.ShadowUniform{})
	uniformLuma := luma()
	if n := slices(); n != 0 {
		t.Fatalf("the uniform fit produced %d cascades; it is a single fit", n)
	}

	r.SetShadows(pix.ShadowCascaded{})
	cascadedLuma := luma()
	if n := slices(); n != pix.DefaultShadowCascades {
		t.Fatalf("switching to cascades produced %d slices, want %d", n, pix.DefaultShadowCascades)
	}

	// Switching back has to release the slices, not leave the cascade atlas in use.
	r.SetShadows(pix.ShadowUniform{})
	backLuma := luma()
	if n := slices(); n != 0 {
		t.Fatalf("switching back to the uniform fit left %d cascades behind", n)
	}
	if backLuma != uniformLuma {
		t.Errorf("returning to the uniform fit did not reproduce its frame: %d then %d",
			uniformLuma, backLuma)
	}

	// Both must actually be shadowing: a fit that silently covered nothing would sail
	// through the checks above.
	r.EnableShadows(false)
	unshadowed := luma()
	if uniformLuma >= unshadowed || cascadedLuma >= unshadowed {
		t.Errorf("a fit cast no shadow: unshadowed=%d uniform=%d cascaded=%d",
			unshadowed, uniformLuma, cascadedLuma)
	}
}

// TestShadowFilterNamesRoundTrip: the console addresses the filter by name, so parse
// and String must agree for every one of them.
func TestShadowFilterNamesRoundTrip(t *testing.T) {
	for _, name := range pix.ShadowFilterNames() {
		f, ok := pix.ParseShadowFilter(name)
		if !ok {
			t.Errorf("ParseShadowFilter(%q) failed on a name it published", name)
			continue
		}
		if got := f.String(); got != name {
			t.Errorf("%q parsed to %v which stringifies as %q", name, uint8(f), got)
		}
	}
	if _, ok := pix.ParseShadowFilter("nonsense"); ok {
		t.Error("an unknown filter name was accepted")
	}
}

// TestShadowFilterDefaultsToHard: the wide kernel costs nine samples per light against
// one, so it is opted into rather than assumed.
func TestShadowFilterDefaultsToHard(t *testing.T) {
	r, err := pix.NewOffscreenRenderer(32, 32)
	if err != nil {
		t.Fatal(err)
	}
	defer r.Destroy()

	if got := r.ShadowFilter(); got != pix.ShadowFilterHard {
		t.Fatalf("default filter = %v, want hard", got)
	}
	r.SetShadowFilter(pix.ShadowFilterSoft)
	if got := r.ShadowFilter(); got != pix.ShadowFilterSoft {
		t.Fatalf("after SetShadowFilter(soft): %v", got)
	}
}

// TestSoftFilterWidensTheShadowEdge is what the wide kernel is for. A single hardware
// tap blends 2x2 texels, so an edge crosses from lit to dark in about a texel; spreading
// nine taps over a 6x6 footprint turns that step into a gradient several pixels wide,
// which is what stops a coarse map reading as stair steps.
//
// Measured as pixels that are PARTLY darkened — neither fully lit nor fully shadowed.
// The fully-dark core has to stay put: a filter that widened the shadow itself would be
// moving it, not softening it.
func TestSoftFilterWidensTheShadowEdge(t *testing.T) {
	const size = 200

	measure := func(filter pix.ShadowFilter) (penumbra, core int) {
		r, err := pix.NewOffscreenRenderer(size, size)
		if err != nil {
			t.Fatal(err)
		}
		defer r.Destroy()
		r.SetClearColor(colors.RGBA32F{0, 0, 0, 1})
		r.SetShadows(pix.ShadowCascaded{})
		r.SetShadowFilter(filter)

		scene, _ := occluderScene(t, r, glm.Vec3f{})
		defer scene.Destroy()

		r.EnableShadows(false)
		r.Render(scene)
		lit := append([]byte(nil), r.Pixels()...)
		r.EnableShadows(true)
		r.Render(scene)
		shadowed := r.Pixels()

		for i := 0; i < len(lit); i += 4 {
			switch d := int(lit[i]) - int(shadowed[i]); {
			case d > 40:
				core++
			case d > 6:
				penumbra++
			}
		}
		return penumbra, core
	}

	hardEdge, hardCore := measure(pix.ShadowFilterHard)
	softEdge, softCore := measure(pix.ShadowFilterSoft)

	if hardCore == 0 {
		t.Fatal("nothing was shadowed; the test measures nothing")
	}
	if softEdge <= 2*hardEdge {
		t.Errorf("the soft filter spread the edge over %d px against the hard filter's %d; "+
			"nine taps over a 6x6 footprint should be markedly wider", softEdge, hardEdge)
	}
	// A tenth is slack for the edge pixels the wider kernel reclassifies, not room for
	// the shadow to move.
	if drift := softCore - hardCore; drift < -hardCore/10 || drift > hardCore/10 {
		t.Errorf("the shadow core moved from %d px to %d px; the filter should soften the "+
			"edge, not relocate the shadow", hardCore, softCore)
	}
}
