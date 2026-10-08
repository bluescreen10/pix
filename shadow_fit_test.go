package pix_test

import (
	"testing"

	"github.com/chewxy/math32"

	"github.com/bluescreen10/pix"
	"github.com/bluescreen10/pix/colors"
	"github.com/bluescreen10/pix/glm"
	"github.com/bluescreen10/pix/scenes"
)

// occluderScene is a ground slab with a cube floating over it, lit by a shadow-casting
// directional light — the smallest scene where a directional shadow is visible. It
// returns the light too, so a test can configure its shadow and ask the renderer for
// the fitted camera.
//
// origin places the whole arrangement, camera included, somewhere in the world. Only the
// far-from-origin test passes anything but the zero vector; a shadow fit must not care
// where in the world it is working, and an earlier fit used to.
//
// The caller owns the scene and must defer its Destroy, so that it runs before the
// renderer's: the scene holds geometry and material references into the renderer's
// stores, and releasing them after those stores are gone panics.
func occluderScene(t *testing.T, r *pix.Renderer, origin glm.Vec3f) (*scenes.Scene, *scenes.DirectionalLight) {
	t.Helper()
	scene := scenes.New()
	scene.SetAmbient(colors.RGB32F{0.05, 0.05, 0.05}, 1)
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
	return scene, light
}

// TestShadowCascadeCount: the number of cascades a light asks for is what decides how
// wide a map to allocate and how many depth passes run, so the renderer has to fit
// exactly that many — clamped to what the light table can carry.
func TestShadowCascadeCount(t *testing.T) {
	r, err := pix.NewOffscreenRenderer(32, 32)
	if err != nil {
		t.Fatal(err)
	}
	defer r.Destroy()

	for _, tc := range []struct {
		set, want int
	}{
		{set: 1, want: 1},
		{set: 2, want: 2},
		{set: 4, want: 4},
		{set: 99, want: scenes.MaxShadowCascades},
		{set: 0, want: 1},
	} {
		splits := fitCascades(t, r, func(shadow *scenes.DirectionalShadow) {
			shadow.SetCascades(tc.set)
		})
		if len(splits) != tc.want {
			t.Errorf("SetCascades(%d) fitted %d cascades, want %d", tc.set, len(splits), tc.want)
		}
	}
}

// fitCascades renders the occluder scene with its light's shadow configured by
// configure, and reports the cascade splits the renderer fitted.
func fitCascades(t *testing.T, r *pix.Renderer, configure func(*scenes.DirectionalShadow)) []float32 {
	t.Helper()
	r.EnableShadows(true)
	scene, light := occluderScene(t, r, glm.Vec3f{})
	defer scene.Destroy()
	configure(light.Shadow())
	r.Render(scene)
	return r.ShadowView(scene.ID(), light.ID()).Splits
}

// TestShadowSplitsFollowTheLight: the splits are fractions of the light's distance,
// measured from the camera's near plane, and the last cascade ends exactly at the
// distance — not at anything derived from the scene.
func TestShadowSplitsFollowTheLight(t *testing.T) {
	r, err := pix.NewOffscreenRenderer(32, 32)
	if err != nil {
		t.Fatal(err)
	}
	defer r.Destroy()

	const near, distance = 0.1, 40 // occluderScene's camera near plane; well inside its far
	splits := fitCascades(t, r, func(shadow *scenes.DirectionalShadow) {
		shadow.SetCascades(4)
		shadow.SetDistance(distance)
		shadow.SetSplits([3]float32{0.05, 0.25, 0.5})
	})
	want := []float32{
		near + 0.05*(distance-near),
		near + 0.25*(distance-near),
		near + 0.5*(distance-near),
		distance,
	}
	if len(splits) != len(want) {
		t.Fatalf("Splits = %v, want %v", splits, want)
	}
	for i := range want {
		if math32.Abs(splits[i]-want[i]) > 1e-3*want[i] {
			t.Errorf("Splits = %v, want %v", splits, want)
			break
		}
	}
}

// TestShadowDistanceStopsAtTheCameraFarPlane: there is nothing to shadow past what the
// camera can see, so a distance beyond its far plane covers only up to it.
func TestShadowDistanceStopsAtTheCameraFarPlane(t *testing.T) {
	r, err := pix.NewOffscreenRenderer(32, 32)
	if err != nil {
		t.Fatal(err)
	}
	defer r.Destroy()

	const far = 100 // occluderScene's camera far plane
	splits := fitCascades(t, r, func(shadow *scenes.DirectionalShadow) {
		shadow.SetDistance(5000)
	})
	if outer := splits[len(splits)-1]; math32.Abs(outer-far) > 1e-3*far {
		t.Errorf("the last cascade ends at %v, want the camera's far plane %v", outer, far)
	}
}

// TestShadowCascadeCountSwitchesAtRuntime: one cascade and four lay out different maps,
// so changing a light's count has to rebuild what the renderer holds for it rather than
// render from whichever layout ran last.
func TestShadowCascadeCountSwitchesAtRuntime(t *testing.T) {
	r, err := pix.NewOffscreenRenderer(160, 160)
	if err != nil {
		t.Fatal(err)
	}
	defer r.Destroy()
	r.EnableShadows(true)
	r.SetClearColor(colors.RGBA32F{0, 0, 0, 1})

	scene, light := occluderScene(t, r, glm.Vec3f{})
	defer scene.Destroy()

	luma := func() int64 {
		r.Render(scene)
		return sceneLuma(r.Pixels())
	}
	slices := func() int {
		return len(r.ShadowView(scene.ID(), light.ID()).Cascades)
	}

	light.Shadow().SetCascades(1)
	singleLuma := luma()
	if n := slices(); n != 1 {
		t.Fatalf("one cascade fitted %d", n)
	}

	light.Shadow().SetCascades(4)
	cascadedLuma := luma()
	if n := slices(); n != 4 {
		t.Fatalf("switching to four cascades fitted %d", n)
	}

	// Switching back has to drop the extra slices, not leave the wider atlas in use.
	light.Shadow().SetCascades(1)
	backLuma := luma()
	if n := slices(); n != 1 {
		t.Fatalf("switching back to one cascade left %d", n)
	}
	if backLuma != singleLuma {
		t.Errorf("returning to one cascade did not reproduce its frame: %d then %d",
			singleLuma, backLuma)
	}

	// Both must actually be shadowing: a fit that silently covered nothing would sail
	// through the checks above.
	r.EnableShadows(false)
	unshadowed := luma()
	if singleLuma >= unshadowed || cascadedLuma >= unshadowed {
		t.Errorf("a fit cast no shadow: unshadowed=%d one=%d four=%d",
			unshadowed, singleLuma, cascadedLuma)
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
