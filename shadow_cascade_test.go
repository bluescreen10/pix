package pix_test

import (
	"math"
	"testing"

	"github.com/bluescreen10/pix"
	"github.com/bluescreen10/pix/colors"
	"github.com/bluescreen10/pix/glm"
	"github.com/bluescreen10/pix/scenes"
)

// bigScene is the case a single shadow map cannot serve: a few hundred units of ground
// with props from under the viewer's feet out to the horizon, and a camera whose near
// plane sits a thousandth of the scene radius away, as examples/beach_lod sets it. The
// caller owns the scene.
func bigScene(t *testing.T, r *pix.Renderer, mapSize uint32) (*scenes.Scene, scenes.Camera, scenes.LightID) {
	t.Helper()
	const radius = 300

	scene := scenes.New()
	scene.SetAmbient(colors.RGB32F{0.35, 0.35, 0.42}, 1)
	light := scene.AddDirectionalLight(glm.Vec3f{-0.5, -1, -0.35}, colors.RGB32F{1, 0.96, 0.9}, 2)
	light.SetCastShadow(true)
	if mapSize != 0 {
		light.Shadow().SetSize(mapSize)
	}

	cube := r.GeometryStore.Create(pix.BoxGeometry(1, 1, 1))
	defer cube.Release()

	ground := scene.NewMesh(cube, r.NewPBRMaterial())
	ground.SetScale(glm.Vec3f{radius * 2, radius * 0.01, radius * 2})
	scene.Add(ground)
	for _, d := range []float32{2, 5, 15, 40, 100, 250} {
		p := scene.NewMesh(cube, r.NewPBRMaterial())
		p.SetPosition(glm.Vec3f{0, d * 0.05, -d})
		p.SetScale(glm.Vec3f{d * 0.05, d * 0.1, d * 0.05})
		p.SetCastShadow(true)
		scene.Add(p)
	}

	cam := scene.NewPerspectiveCamera(45, 1, radius*0.001, radius*12)
	scene.Add(cam)
	cam.SetPosition(glm.Vec3f{0, 1.7, 8})
	cam.LookAt(glm.Vec3f{0, 1.2, -20})
	return scene, cam, light.ID()
}

// TestCascadesResolveTheNearFieldInALargeScene is the reason this algorithm exists. One
// map stretched over a few hundred units puts well under a texel per screen pixel on the
// ground at the viewer's feet, which is a smudge however it is fitted or warped; slicing
// the range means the nearest slice covers a few units and its texels land where they
// can be seen.
//
// The measurement is analytic rather than pictorial: cast a ray through a screen pixel
// onto the ground, ask where that point lands in the shadow map, and compare against its
// neighbour. Below one texel per pixel the map is coarser than the screen.
func TestCascadesResolveTheNearFieldInALargeScene(t *testing.T) {
	const size = 256
	const mapSize = 1024

	density := func(algo pix.ShadowSettings) (across, down float64) {
		r, err := pix.NewOffscreenRenderer(size, size)
		if err != nil {
			t.Fatal(err)
		}
		defer r.Destroy()
		r.SetClearColor(colors.RGBA32F{0, 0, 0, 1})
		r.SetShadows(algo)
		r.EnableShadows(true)

		scene, cam, lightID := bigScene(t, r, mapSize)
		defer scene.Destroy()
		r.Render(scene)

		view := r.ShadowView(scene.ID(), lightID)
		if view == nil {
			t.Fatalf("%v: the renderer fitted no shadow camera", algo)
		}
		// view.Camera is the innermost cascade when there are cascades, which is the one
		// covering the ground being probed.
		shadowVP := view.Camera.ViewProjection()
		inv := cam.ViewProjection().Inv()

		at := func(sx, sy float32) (glm.Vec2f, bool) {
			ndc := glm.Vec4f{2*sx/size - 1, 1 - 2*sy/size, 1, 1}
			a := inv.Mul4x1(ndc)
			b := inv.Mul4x1(glm.Vec4f{ndc[0], ndc[1], 0, 1})
			near := glm.Vec3f{a[0] / a[3], a[1] / a[3], a[2] / a[3]}
			far := glm.Vec3f{b[0] / b[3], b[1] / b[3], b[2] / b[3]}
			dir := far.Sub(near)
			if dir[1] == 0 {
				return glm.Vec2f{}, false
			}
			hit := -near[1] / dir[1] // the ground is the plane y = 0
			if hit < 0 || hit > 1 {
				return glm.Vec2f{}, false
			}
			w := near.Add(dir.Scale(hit))
			c := shadowVP.Mul4x1(glm.Vec4f{w[0], w[1], w[2], 1})
			if c[3] <= 0 {
				return glm.Vec2f{}, false
			}
			// Each cascade's matrix maps into its own square of the atlas, so the square's
			// side is the resolution that matters, not the whole texture's width.
			return glm.Vec2f{(c[0]/c[3]*0.5 + 0.5) * mapSize, (c[1]/c[3]*0.5 + 0.5) * mapSize}, true
		}

		const row = size - 6 // the closest ground the camera can see
		c, ok := at(size/2, row)
		right, okR := at(size/2+1, row)
		below, okD := at(size/2, row-1)
		if !ok || !okR || !okD {
			t.Fatalf("%v: the sampled pixels do not land on the ground", algo)
		}
		return math.Hypot(float64(right[0]-c[0]), float64(right[1]-c[1])),
			math.Hypot(float64(below[0]-c[0]), float64(below[1]-c[1]))
	}

	uniformAcross, uniformDown := density(pix.ShadowUniform{})
	across, down := density(pix.ShadowCascaded{})

	if worst := min(across, down); worst < 8*min(uniformAcross, uniformDown) {
		t.Errorf("cascades put %.2f/%.2f texels per screen pixel on the near ground against "+
			"a single map's %.2f/%.2f; slicing the range is supposed to be transformative here",
			across, down, uniformAcross, uniformDown)
	}
}

// TestCascadesMatchOneMapInASmallScene: when the whole scene fits comfortably in one
// map there is nothing for the split to win, and cascades must not make things worse.
// Both are the same orthographic fit, so they should agree closely.
func TestCascadesMatchOneMapInASmallScene(t *testing.T) {
	const size = 128

	area := func(algo pix.ShadowSettings) int {
		r, err := pix.NewOffscreenRenderer(size, size)
		if err != nil {
			t.Fatal(err)
		}
		defer r.Destroy()
		r.SetClearColor(colors.RGBA32F{0, 0, 0, 1})
		r.SetShadows(algo)

		scene, _ := occluderScene(t, r, glm.Vec3f{})
		defer scene.Destroy()

		r.EnableShadows(false)
		r.Render(scene)
		lit := append([]byte(nil), r.Pixels()...)
		r.EnableShadows(true)
		r.Render(scene)
		shadowed := r.Pixels()

		n := 0
		for i := 0; i < len(lit); i += 4 {
			if int(lit[i])-int(shadowed[i]) > 12 {
				n++
			}
		}
		return n
	}

	one, split := area(pix.ShadowUniform{}), area(pix.ShadowCascaded{})
	if one == 0 {
		t.Fatal("the single-map fit shadowed nothing; the test measures nothing")
	}
	if diff := math.Abs(float64(split-one)) / float64(one); diff > 0.15 {
		t.Errorf("cascades shadowed %d px against one map's %d (%.0f%% apart); on a scene this "+
			"small the split should change almost nothing", split, one, 100*diff)
	}
}

// TestCascadeCountRoundTrips: the count is a memory/quality dial — every extra slice is
// another square of shadow map and another depth pass — so it has to be settable, and
// out-of-range values must not silently allocate something enormous.
func TestCascadeCountRoundTrips(t *testing.T) {
	r, err := pix.NewOffscreenRenderer(32, 32)
	if err != nil {
		t.Fatal(err)
	}
	defer r.Destroy()

	r.SetShadows(pix.ShadowCascaded{Levels: 2})
	cs, ok := r.Shadows().(pix.ShadowCascaded)
	if !ok || cs.Levels != 2 {
		t.Errorf("settings came back as %#v", r.Shadows())
	}
}

// TestCascadesRenderEverySlice guards the atlas plumbing. The slices share one texture
// and only the first clears it, so a mistake there — the wrong scissor, every view
// clearing, a load op that discards — shows up as cascades quietly missing rather than
// as an error.
//
// The scene is deliberately small. On a large one a single cascade is the very case
// cascades exist to fix, and its shadow would be too coarse to detect at all, so the
// test would be measuring the smudge rather than the plumbing.
func TestCascadesRenderEverySlice(t *testing.T) {
	for count := uint32(1); count <= pix.MaxShadowCascades; count++ {
		r, err := pix.NewOffscreenRenderer(128, 128)
		if err != nil {
			t.Fatal(err)
		}
		r.SetClearColor(colors.RGBA32F{0, 0, 0, 1})
		r.SetShadows(pix.ShadowCascaded{})
		r.SetShadows(pix.ShadowCascaded{Levels: int(count)})

		scene, lightID := occluderScene(t, r, glm.Vec3f{})

		r.EnableShadows(false)
		r.Render(scene)
		lit := append([]byte(nil), r.Pixels()...)
		r.EnableShadows(true)
		r.Render(scene)
		shadowed := r.Pixels()

		n := 0
		for i := 0; i < len(lit); i += 4 {
			if int(lit[i])-int(shadowed[i]) > 12 {
				n++
			}
		}
		if n == 0 {
			t.Errorf("%d cascades shadowed nothing", count)
		}
		if view := r.ShadowView(scene.ID(), lightID); view == nil {
			t.Errorf("%d cascades: no shadow view", count)
		} else if got := uint32(len(view.Cascades)); got != count {
			t.Errorf("asked for %d cascades, the fit produced %d", count, got)
		}
		scene.Destroy()
		r.Destroy()
	}
}

// TestShadowBiasHoldsAsTheLightGrazes pins the angle-dependent bias. A shadow texel's
// footprint along the light stretches as the surface turns away from it, so the depth
// error inside one texel grows without bound while a constant bias does not — which
// shows up as acne on surfaces that should be lit, worst where the light is most
// oblique. Scaling the offsets by sin and tan of the angle between the normal and the
// light is what bounds it (see shadowOffsets in lighting.glsl).
//
// Acne is counted as shadowed pixels that are isolated or one pixel wide: a real shadow
// is a connected region, speckle is not.
func TestShadowBiasHoldsAsTheLightGrazes(t *testing.T) {
	const size = 220

	speckleFraction := func(elevation float32) (float64, int) {
		r, err := pix.NewOffscreenRenderer(size, size)
		if err != nil {
			t.Fatal(err)
		}
		defer r.Destroy()
		r.SetClearColor(colors.RGBA32F{0, 0, 0, 1})
		r.SetShadows(pix.ShadowCascaded{})

		scene := scenes.New()
		defer scene.Destroy()
		scene.SetAmbient(colors.RGB32F{0.05, 0.05, 0.05}, 1)
		light := scene.AddDirectionalLight(
			glm.Vec3f{0.3, -elevation, 0.5}.Normalize(), colors.RGB32F{1, 1, 1}, 3)
		light.SetCastShadow(true)
		light.Shadow().SetSize(1024)

		cube := r.GeometryStore.Create(pix.BoxGeometry(1, 1, 1))
		defer cube.Release()
		ground := scene.NewMesh(cube, r.NewPBRMaterial())
		ground.SetScale(glm.Vec3f{60, 0.2, 60})
		scene.Add(ground)
		// A tilted ramp and an upright wall, so several surface angles meet the light at
		// once and no single constant bias can suit all of them.
		ramp := scene.NewMesh(cube, r.NewPBRMaterial())
		ramp.SetPosition(glm.Vec3f{-3, 1, -2})
		ramp.SetScale(glm.Vec3f{4, 0.3, 6})
		ramp.SetRotation(glm.Vec3f{0.4, 0, 0.3})
		ramp.SetCastShadow(true)
		scene.Add(ramp)
		wall := scene.NewMesh(cube, r.NewPBRMaterial())
		wall.SetPosition(glm.Vec3f{4, 1.5, -4})
		wall.SetScale(glm.Vec3f{0.4, 3, 8})
		wall.SetCastShadow(true)
		scene.Add(wall)
		box := scene.NewMesh(cube, r.NewPBRMaterial())
		box.SetPosition(glm.Vec3f{0, 1, 2})
		box.SetScale(glm.Vec3f{1.2, 2, 1.2})
		box.SetCastShadow(true)
		scene.Add(box)

		cam := scene.NewPerspectiveCamera(55, 1, 0.3, 200)
		scene.Add(cam)
		cam.SetPosition(glm.Vec3f{2, 5, 12})
		cam.LookAt(glm.Vec3f{0, 1, -2})

		r.EnableShadows(false)
		r.Render(scene)
		lit := append([]byte(nil), r.Pixels()...)
		r.EnableShadows(true)
		r.Render(scene)
		shadowed := r.Pixels()

		dark := make([]bool, size*size)
		for i := range dark {
			dark[i] = int(lit[i*4])-int(shadowed[i*4]) > 10
		}
		var speckle, total int
		for y := 1; y < size-1; y++ {
			for x := 1; x < size-1; x++ {
				i := y*size + x
				if !dark[i] {
					continue
				}
				total++
				neighbours := 0
				for _, d := range []int{-1, 1, -size, size} {
					if dark[i+d] {
						neighbours++
					}
				}
				if neighbours <= 1 {
					speckle++
				}
			}
		}
		if total == 0 {
			return 0, 0
		}
		return 100 * float64(speckle) / float64(total), total
	}

	// Overhead down to a low sun. Lower than this and the shadows leave the frame, which
	// would measure nothing however the bias behaves.
	for _, elevation := range []float32{2.0, 1.0, 0.5, 0.3} {
		got, total := speckleFraction(elevation)
		if total < 500 {
			t.Fatalf("elevation %.2f shadowed only %d px; the test measures nothing", elevation, total)
		}
		if got > 2.5 {
			t.Errorf("at sun elevation %.2f, %.1f%% of shadowed pixels are isolated speckle; "+
				"the bias is not tracking how obliquely the light meets the surface", elevation, got)
		}
	}
}

// TestShadowNearControlsTheCascadeRatio pins the knob that decides how hard quality
// falls at a boundary.
//
// A logarithmic split covers [near, far] in equal ratios, so consecutive cascades differ
// by (far/near)^(1/n) and texel density drops by exactly that at every boundary — the
// step a thin shadow visibly breaks up on. That makes the ratio the thing worth
// controlling, and it is set as much by where the split STARTS as by where it ends.
//
// This pins the part that was once broken: the derived near scaled with the far
// distance, which cancelled out of the ratio entirely and left it fixed at the same
// value however the shadow distance was set. Shortening the range has to help, and
// starting the split further out has to help, or neither knob does anything.
func TestShadowNearControlsTheCascadeRatio(t *testing.T) {
	ratio := func(near, distance float32) float32 {
		r, err := pix.NewOffscreenRenderer(128, 128)
		if err != nil {
			t.Fatal(err)
		}
		defer r.Destroy()
		r.SetShadows(pix.ShadowCascaded{})
		r.SetShadowNear(near)
		r.SetShadowDistance(distance)
		r.EnableShadows(true)

		scene, _, lightID := bigScene(t, r, 1024)
		defer scene.Destroy()
		r.Render(scene)

		view := r.ShadowView(scene.ID(), lightID)
		if view == nil || len(view.Splits) < 2 {
			t.Fatal("the fit produced no cascade splits")
		}
		return view.Splits[1] / view.Splits[0]
	}

	// The auto range is the whole visible scene, hundreds of units deep.
	wide := ratio(0, 0)
	if short := ratio(0, 60); short >= wide {
		t.Errorf("shortening the shadow distance left the cascade ratio at %.2fx (was %.2fx); "+
			"the split's near distance is still scaling with its far distance", short, wide)
	}
	if out := ratio(8, 0); out >= wide {
		t.Errorf("starting the split at 8 units left the ratio at %.2fx (was %.2fx)", out, wide)
	}
	// Both together are what actually tames it.
	if both := ratio(4, 60); both > wide/2 {
		t.Errorf("a short range started well out still gives a %.2fx ratio, against %.2fx "+
			"wide open; the two knobs should compound", both, wide)
	}
}

// TestShadowNearDoesNotDropNearGeometry: the split's near distance decides where
// BOUNDARIES fall, not what is covered. The first cascade is still fitted from the
// camera's own near plane, so pushing the split out must not stop shadowing whatever
// lies in between.
func TestShadowNearDoesNotDropNearGeometry(t *testing.T) {
	area := func(near float32) int {
		r, err := pix.NewOffscreenRenderer(160, 160)
		if err != nil {
			t.Fatal(err)
		}
		defer r.Destroy()
		r.SetClearColor(colors.RGBA32F{0, 0, 0, 1})
		r.SetShadows(pix.ShadowCascaded{})
		r.SetShadowNear(near)

		scene, _ := occluderScene(t, r, glm.Vec3f{})
		defer scene.Destroy()

		r.EnableShadows(false)
		r.Render(scene)
		lit := append([]byte(nil), r.Pixels()...)
		r.EnableShadows(true)
		r.Render(scene)
		shadowed := r.Pixels()

		n := 0
		for i := 0; i < len(lit); i += 4 {
			if int(lit[i])-int(shadowed[i]) > 12 {
				n++
			}
		}
		return n
	}

	base := area(0)
	if base == 0 {
		t.Fatal("nothing was shadowed with the derived near; the test measures nothing")
	}
	// The occluder in this scene sits a handful of units from the eye, so a split
	// starting past it is exactly the case that would drop it.
	if got := area(20); got < base*3/4 {
		t.Errorf("with the split starting at 20 units the shadow fell to %d px from %d; "+
			"the first cascade is meant to cover everything nearer than the split", got, base)
	}
}

// TestExplicitStepsAreHonoured: tuning a scene means choosing the boundaries, so they
// have to arrive unaltered rather than being treated as a hint.
//
// What they buy is the texel size of the outermost slice, which is what decides whether
// something thin survives out there: a derived split over a scene a few hundred units
// deep hands its last cascade a range so wide that its texels are larger than the
// geometry casting into them, and a cable or a railing stops registering at all rather
// than merely softening.
func TestExplicitStepsAreHonoured(t *testing.T) {
	// The fitted half-width of a cascade, recovered from its matrix. An orthographic
	// projection scales x by 1/halfWidth, and this matrix is projection times view, so
	// the scale is the row's LENGTH — its first component alone would carry the light
	// basis rotation too.
	texel := func(cam pix.Camera, mapSide float32) float32 {
		row := cam.ViewProjection().Row(0)
		return 2 / (glm.Vec3f{row[0], row[1], row[2]}.Length() * mapSide)
	}

	// One renderer at a time. Holding two alive and tearing them down together is not
	// something a renderer should mind, but it is not what this test is about, and it
	// currently takes the Vulkan backend's device teardown down with it — so the numbers
	// come out before the renderer goes away.
	fit := func(set pix.ShadowSettings) (splits []float32, outerTexel float32) {
		r, err := pix.NewOffscreenRenderer(256, 256)
		if err != nil {
			t.Fatal(err)
		}
		defer r.Destroy()
		r.SetShadows(set)
		r.EnableShadows(true)

		scene, _, id := bigScene(t, r, 1024)
		defer scene.Destroy()
		r.Render(scene)

		view := r.ShadowView(scene.ID(), id)
		if view == nil || len(view.Cascades) == 0 {
			t.Fatal("the renderer fitted no shadow camera")
		}
		return append([]float32(nil), view.Splits...), texel(view.Cascades[len(view.Cascades)-1], 1024)
	}

	steps := []float32{8, 25, 80}
	explicitSplits, explicitOuter := fit(pix.ShadowCascaded{Steps: steps})

	if len(explicitSplits) != len(steps) {
		t.Fatalf("asked for %d steps, got %d slices", len(steps), len(explicitSplits))
	}
	for i, want := range steps {
		if got := explicitSplits[i]; got != want {
			t.Errorf("step %d came back as %v, want %v", i, got, want)
		}
	}

	// Against the derived split over the same scene, whose outermost slice spans most of
	// it and is correspondingly coarse.
	_, derivedOuter := fit(pix.ShadowCascaded{Levels: len(steps)})

	if explicitOuter >= derivedOuter/2 {
		t.Errorf("the outermost slice's texel is %.3f world units against the derived "+
			"split's %.3f; choosing the boundaries is supposed to be what reins that in",
			explicitOuter, derivedOuter)
	}
}

// TestExplicitStepsSetTheShadowRange: the outermost step is where shadows stop, so it
// has to decide how far the fit reaches rather than be clipped by a range chosen
// elsewhere.
//
// Left to the derived range, every step past it was silently clamped to the frustum's
// far edge — so asking for boundaries beyond it produced two or three cascades fitted to
// the same sliver, each costing a depth pass and a square of map while covering nothing
// new. What that looks like from outside is geometry well inside the requested range
// falling outside the cascade that claims it, which is what this checks.
func TestExplicitStepsSetTheShadowRange(t *testing.T) {
	// bigScene is a few hundred units across, so these run well past whatever range
	// would have been derived for it.
	steps := []float32{900, 1200, 3000}

	r, err := pix.NewOffscreenRenderer(256, 256)
	if err != nil {
		t.Fatal(err)
	}
	defer r.Destroy()
	r.SetShadows(pix.ShadowCascaded{Steps: steps})
	r.EnableShadows(true)

	scene, _, id := bigScene(t, r, 1024)
	defer scene.Destroy()
	r.Render(scene)

	view := r.ShadowView(scene.ID(), id)
	if view == nil || len(view.Splits) != len(steps) {
		t.Fatalf("the fit produced %d slices, want %d", len(view.Splits), len(steps))
	}
	for i, want := range steps {
		if got := view.Splits[i]; got != want {
			t.Errorf("step %d came back as %v, want %v", i, got, want)
		}
	}

	// Each slice covers further than the last, so its fit can only be as coarse or
	// coarser. Clamping broke exactly that: the outer steps collapsed onto a sliver at
	// the old range's far edge and came back FINER than the slice before them, which is
	// the signature of a cascade that was never given the range it asked for.
	//
	// Equality is allowed. The fitted radius is quantized in quarter-octave steps so two
	// adjacent slices of similar size legitimately land in one bucket, and a slice that
	// reaches past the geometry is capped to the scene's own bounds rather than growing
	// to cover empty space.
	texel := func(cam pix.Camera) float32 {
		row := cam.ViewProjection().Row(0)
		return 2 / (glm.Vec3f{row[0], row[1], row[2]}.Length() * 1024)
	}
	for i := 1; i < len(view.Cascades); i++ {
		prev, cur := texel(view.Cascades[i-1]), texel(view.Cascades[i])
		if cur < prev {
			t.Errorf("slice %d reaches to %v yet its texel is %.3f, finer than slice %d's "+
				"%.3f at %v; it was clamped to a shorter range instead of setting one",
				i, view.Splits[i], cur, i-1, prev, view.Splits[i-1])
		}
	}
}

// TestCascadeAtlasStaysWithinTextureLimits pins a crash, not a quality problem.
//
// Cascades lay their squares out along the width, so the texture asked for is the
// requested resolution MULTIPLIED by the number of slices. Four cascades of a 4096 map
// is 16384 wide, which is exactly the limit on the common backends, and anything past it
// is not a soft failure — the driver asserts and takes the process down. The atlas has
// to step the per-slice resolution down instead, keeping the squares square.
func TestCascadeAtlasStaysWithinTextureLimits(t *testing.T) {
	for _, size := range []uint32{1024, 4096, 8192, 16384} {
		for _, levels := range []int{1, 2, 3, 4} {
			r, err := pix.NewOffscreenRenderer(64, 64)
			if err != nil {
				t.Fatal(err)
			}
			r.SetShadows(pix.ShadowCascaded{Levels: levels})
			r.EnableShadows(true)

			// bigScene asks the light for the resolution, which is what the atlas
			// multiplies by the slice count.
			scene, _, id := bigScene(t, r, size)
			// Rendering at all is most of the test: an atlas over the limit aborts the
			// process rather than returning an error.
			r.Render(scene)

			if view := r.ShadowView(scene.ID(), id); view == nil {
				t.Errorf("%d texels x %d levels: no shadow view", size, levels)
			} else if got := len(view.Cascades); got != levels {
				t.Errorf("%d texels x %d levels: produced %d slices", size, levels, got)
			}
			scene.Destroy()
			r.Destroy()
		}
	}
}
