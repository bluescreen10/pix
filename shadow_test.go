package pix

import (
	"testing"

	"github.com/bluescreen10/pix/cameras"
	"github.com/bluescreen10/pix/colors"
	"github.com/bluescreen10/pix/glm"
)

// TestDirectionalShadowMapAllocated checks the Phase 1 infrastructure: with shadows
// enabled, a shadow-casting directional light gets a bindless depth map allocated (and
// its ortho camera fitted) on the first render.
func TestDirectionalShadowMapAllocated(t *testing.T) {
	r, err := NewOffscreenRenderer(64, 64)
	if err != nil {
		t.Fatal(err)
	}
	defer r.Destroy()
	r.EnableShadows(true)

	scene := r.NewScene()
	defer scene.Destroy()
	light := scene.AddDirectionalLight(glm.Vec3f{-0.4, -1, -0.3}, colors.RGB32F{1, 1, 1}, 1)
	light.SetCastShadow(true)

	cube := r.GeometryStore.Create(normalCube())
	defer cube.Release()
	scene.Add(scene.NewMesh(cube, r.NewPBRMaterial()))

	cam := cameras.NewPerspectiveCamera(45, 1, 0.1, 100)
	cam.SetPosition(glm.Vec3f{0, 0, 3})
	r.Render(scene, cam)

	s := light.Shadow()
	if s == nil {
		t.Fatal("light.Shadow() is nil after SetCastShadow(true)")
	}
	if !s.Map.Valid() {
		t.Fatal("shadow map not allocated after render with shadows enabled")
	}
	// A fitted ortho camera should look at the scene (non-zero view-projection).
	if s.Camera.ViewProjection() == (glm.Mat4f{}) {
		t.Fatal("shadow camera view-projection is zero — not fitted")
	}
	t.Logf("shadow map heap index=%d size=%d", s.Map.Index(), s.Size())
}

// TestDirectionalShadowDepthPass drives the Stage B path end to end: a shadow-casting
// mesh forces the shadow view's cull (castersOnly) to find a caster and the depth pass
// to draw into the light's map. It renders two frames so the map's SHADER_READ →
// DEPTH_ATTACHMENT transition on the second frame is exercised, then confirms the main
// color pass still produced the scene (the extra views didn't break the frame).
func TestDirectionalShadowDepthPass(t *testing.T) {
	r, err := NewOffscreenRenderer(96, 96)
	if err != nil {
		t.Fatal(err)
	}
	defer r.Destroy()
	r.EnableShadows(true)
	r.SetClearColor(colors.RGBA32F{0, 0, 0, 1})

	scene := r.NewScene()
	defer scene.Destroy()
	scene.SetAmbient(colors.RGB32F{0.3, 0.3, 0.3})
	light := scene.AddDirectionalLight(glm.Vec3f{-0.4, -1, -0.3}, colors.RGB32F{1, 1, 1}, 2)
	light.SetCastShadow(true)

	cube := r.GeometryStore.Create(normalCube())
	defer cube.Release()
	mesh := scene.NewMesh(cube, r.NewPBRMaterial())
	mesh.SetCastShadow(true) // so the shadow view's castersOnly cull keeps it
	scene.Add(mesh)

	cam := cameras.NewPerspectiveCamera(45, 1, 0.1, 100)
	cam.SetPosition(glm.Vec3f{0, 0, 3})

	// Two frames: frame 2 transitions the shadow map back from sampled to depth.
	r.Render(scene, cam)
	r.Render(scene, cam)

	// The main pass must still have drawn the lit cube — assert some pixel is non-black.
	px := r.Pixels()
	if len(px) == 0 {
		t.Fatal("no pixels captured")
	}
	var lit bool
	for i := 0; i+3 < len(px); i += 4 {
		if px[i] > 8 || px[i+1] > 8 || px[i+2] > 8 {
			lit = true
			break
		}
	}
	if !lit {
		t.Fatal("main color pass produced an all-black frame with shadows enabled — the shadow views broke the frame")
	}
}

// TestShadowsEmptyScene guards the degenerate case: shadows enabled with a casting
// directional light but NO geometry. The draw list has no batches, so shadow views
// must be skipped entirely (they'd otherwise allocate a zero-sized visible buffer).
func TestShadowsEmptyScene(t *testing.T) {
	r, err := NewOffscreenRenderer(64, 64)
	if err != nil {
		t.Fatal(err)
	}
	defer r.Destroy()
	r.EnableShadows(true)

	scene := r.NewScene()
	defer scene.Destroy()
	light := scene.AddDirectionalLight(glm.Vec3f{-0.4, -1, -0.3}, colors.RGB32F{1, 1, 1}, 1)
	light.SetCastShadow(true)

	cam := cameras.NewPerspectiveCamera(45, 1, 0.1, 100)
	cam.SetPosition(glm.Vec3f{0, 0, 3})
	r.Render(scene, cam) // must not panic on the empty draw list
}

// TestSpotShadowDarkensReceiver is the spot-light analogue of the directional test: a
// spot cone aimed down at a ground slab with an occluder cube must cast a visible
// shadow, so the same scene renders darker with shadows on than off.
func TestSpotShadowDarkensReceiver(t *testing.T) {
	build := func(shadows bool) []byte {
		r, err := NewOffscreenRenderer(160, 160)
		if err != nil {
			t.Fatal(err)
		}
		defer r.Destroy()
		r.EnableShadows(shadows)
		r.SetClearColor(colors.RGBA32F{0, 0, 0, 1})

		scene := r.NewScene()
		defer scene.Destroy()
		scene.SetAmbient(colors.RGB32F{0.04, 0.04, 0.04})
		// Spot up and to the side, aimed at the scene center, so the occluder's shadow
		// falls offset onto the ground where the camera can see it (a straight-down spot
		// would hide its own shadow behind the occluder).
		pos := glm.Vec3f{3, 6, 2}
		dir := glm.Vec3f{0, 0, 0}.Sub(pos).Normalize()
		spot := scene.AddSpotLight(pos, dir, colors.RGB32F{1, 1, 1}, 6, 30, 0.7, 0.2)
		spot.SetCastShadow(true)

		cube := r.GeometryStore.Create(normalCube())
		defer cube.Release()

		ground := scene.NewMesh(cube, r.NewPBRMaterial())
		ground.SetScale(glm.Vec3f{6, 0.2, 6})
		scene.Add(ground)

		occluder := scene.NewMesh(cube, r.NewPBRMaterial())
		occluder.SetPosition(glm.Vec3f{0, 1.5, 0})
		occluder.SetScale(glm.Vec3f{0.8, 0.8, 0.8})
		occluder.SetCastShadow(true)
		scene.Add(occluder)

		cam := cameras.NewPerspectiveCamera(45, 1, 0.1, 100)
		cam.SetPosition(glm.Vec3f{0, 5, 6})
		cam.SetTarget(glm.Vec3f{0, 0, 0})

		r.Render(scene, cam)
		out := make([]byte, len(r.Pixels()))
		copy(out, r.Pixels())
		return out
	}

	lit := build(false)
	shadowed := build(true)
	litLuma, shadowLuma := sceneLuma(lit), sceneLuma(shadowed)
	if shadowLuma >= litLuma {
		t.Fatalf("spot shadow not applied: shadows-off luma=%d, shadows-on luma=%d", litLuma, shadowLuma)
	}
	t.Logf("spot luma off=%d on=%d (%.1f%% darker)", litLuma, shadowLuma,
		100*float64(litLuma-shadowLuma)/float64(litLuma))
}

// TestPointShadowDarkensReceiver drives point-light cube shadows: a point light with an
// occluder between it and a ground slab must cast a visible shadow, so the same scene
// renders darker with shadows on than off. Exercises the 6-face path + face selection.
func TestPointShadowDarkensReceiver(t *testing.T) {
	build := func(shadows bool) []byte {
		r, err := NewOffscreenRenderer(160, 160)
		if err != nil {
			t.Fatal(err)
		}
		defer r.Destroy()
		r.EnableShadows(shadows)
		r.SetClearColor(colors.RGBA32F{0, 0, 0, 1})

		scene := r.NewScene()
		defer scene.Destroy()
		scene.SetAmbient(colors.RGB32F{0.04, 0.04, 0.04})
		// Point light up and to the side so the occluder's shadow lands offset on the
		// ground within the camera's view.
		pl := scene.AddPointLight(glm.Vec3f{3, 5, 2}, colors.RGB32F{1, 1, 1}, 8, 40)
		pl.SetCastShadow(true)

		cube := r.GeometryStore.Create(normalCube())
		defer cube.Release()

		ground := scene.NewMesh(cube, r.NewPBRMaterial())
		ground.SetScale(glm.Vec3f{6, 0.2, 6})
		scene.Add(ground)

		occluder := scene.NewMesh(cube, r.NewPBRMaterial())
		occluder.SetPosition(glm.Vec3f{0, 1.5, 0})
		occluder.SetScale(glm.Vec3f{0.8, 0.8, 0.8})
		occluder.SetCastShadow(true)
		scene.Add(occluder)

		cam := cameras.NewPerspectiveCamera(45, 1, 0.1, 100)
		cam.SetPosition(glm.Vec3f{0, 5, 6})
		cam.SetTarget(glm.Vec3f{0, 0, 0})

		r.Render(scene, cam)
		out := make([]byte, len(r.Pixels()))
		copy(out, r.Pixels())
		return out
	}

	lit := build(false)
	shadowed := build(true)
	litLuma, shadowLuma := sceneLuma(lit), sceneLuma(shadowed)
	if shadowLuma >= litLuma {
		t.Fatalf("point shadow not applied: shadows-off luma=%d, shadows-on luma=%d", litLuma, shadowLuma)
	}
	t.Logf("point luma off=%d on=%d (%.1f%% darker)", litLuma, shadowLuma,
		100*float64(litLuma-shadowLuma)/float64(litLuma))
}

// sceneLuma sums the luminance of a captured RGBA8 frame.
func sceneLuma(px []byte) int64 {
	var sum int64
	for i := 0; i+3 < len(px); i += 4 {
		sum += int64(px[i]) + int64(px[i+1]) + int64(px[i+2])
	}
	return sum
}

// TestDirectionalShadowDarkensReceiver is the Stage C payoff: an occluder cube above
// a ground slab, lit by a directional light, must cast a visible shadow. Rendering the
// exact same scene with shadows on vs off, the shadowed frame is strictly darker (the
// occluded ground loses its directional contribution) — proving the light table's
// shadowVP/map + the PCF sampling actually attenuate light.
func TestDirectionalShadowDarkensReceiver(t *testing.T) {
	build := func(shadows bool) []byte {
		r, err := NewOffscreenRenderer(160, 160)
		if err != nil {
			t.Fatal(err)
		}
		defer r.Destroy()
		r.EnableShadows(shadows)
		r.SetClearColor(colors.RGBA32F{0, 0, 0, 1})

		scene := r.NewScene()
		defer scene.Destroy()
		scene.SetAmbient(colors.RGB32F{0.05, 0.05, 0.05}) // low fill so the shadow is visible
		light := scene.AddDirectionalLight(glm.Vec3f{0.15, -1, 0.15}, colors.RGB32F{1, 1, 1}, 3)
		light.SetCastShadow(true)

		cube := r.GeometryStore.Create(normalCube())
		defer cube.Release()

		ground := scene.NewMesh(cube, r.NewPBRMaterial())
		ground.SetPosition(glm.Vec3f{0, 0, 0})
		ground.SetScale(glm.Vec3f{6, 0.2, 6}) // wide flat receiver
		scene.Add(ground)

		occluder := scene.NewMesh(cube, r.NewPBRMaterial())
		occluder.SetPosition(glm.Vec3f{0, 1.5, 0})
		occluder.SetScale(glm.Vec3f{0.8, 0.8, 0.8})
		occluder.SetCastShadow(true)
		scene.Add(occluder)

		cam := cameras.NewPerspectiveCamera(45, 1, 0.1, 100)
		cam.SetPosition(glm.Vec3f{0, 5, 6})
		cam.SetTarget(glm.Vec3f{0, 0, 0})

		r.Render(scene, cam)
		out := make([]byte, len(r.Pixels()))
		copy(out, r.Pixels())
		return out
	}

	lit := build(false)
	shadowed := build(true)
	litLuma, shadowLuma := sceneLuma(lit), sceneLuma(shadowed)
	if shadowLuma >= litLuma {
		t.Fatalf("shadowed frame not darker: shadows-off luma=%d, shadows-on luma=%d (shadow not applied)", litLuma, shadowLuma)
	}
	t.Logf("luma shadows-off=%d shadows-on=%d (%.1f%% darker)", litLuma, shadowLuma,
		100*float64(litLuma-shadowLuma)/float64(litLuma))
}

// TestShadowSetSizeReallocatesMap covers the accessor's contract: SetSize after the map
// already exists must actually reallocate it, not be silently ignored. Size is also
// read every frame for the bias and the shadow pass viewport, so a stale map would
// leave those disagreeing with the texture they describe.
func TestShadowSetSizeReallocatesMap(t *testing.T) {
	r, err := NewOffscreenRenderer(64, 64)
	if err != nil {
		t.Fatal(err)
	}
	defer r.Destroy()
	r.EnableShadows(true)

	scene := r.NewScene()
	defer scene.Destroy()
	light := scene.AddDirectionalLight(glm.Vec3f{-0.4, -1, -0.3}, colors.RGB32F{1, 1, 1}, 1)
	light.SetCastShadow(true)

	cube := r.GeometryStore.Create(normalCube())
	defer cube.Release()
	scene.Add(scene.NewMesh(cube, r.NewPBRMaterial()))

	cam := cameras.NewPerspectiveCamera(45, 1, 0.1, 100)
	cam.SetPosition(glm.Vec3f{0, 0, 3})
	r.Render(scene, cam)

	s := light.Shadow()
	if s.Size() != defaultShadowSize {
		t.Fatalf("default size = %d, want %d", s.Size(), defaultShadowSize)
	}
	first := s.Map.Index()

	// Same size: the map must be left alone.
	s.SetSize(defaultShadowSize)
	r.Render(scene, cam)
	if s.Map.Index() != first {
		t.Error("map reallocated even though the size did not change")
	}

	// A zero size is ignored rather than producing an invalid texture.
	s.SetSize(0)
	if s.Size() != defaultShadowSize {
		t.Fatalf("SetSize(0) changed size to %d", s.Size())
	}

	// A new size must take effect.
	s.SetSize(512)
	r.Render(scene, cam)
	if s.Size() != 512 {
		t.Fatalf("size = %d after SetSize(512)", s.Size())
	}
	if s.Map.Index() == first {
		t.Fatal("map was not reallocated after SetSize changed the resolution")
	}
	if !s.Map.Valid() {
		t.Fatal("map invalid after resize")
	}
}

// TestEnableShadowsTogglesAtRuntime covers turning shadows off on a renderer that has
// already drawn with them on — what a console `set shadows off` does, and what every
// other test in this file misses by building a fresh renderer per configuration.
//
// The failure it guards is specific: disabling shadows stops preparing and rendering
// the shadow maps, but the per-light GPU records still carried the heap index of the
// last map drawn, so the lighting shader kept sampling it. Shadows froze on screen
// instead of disappearing.
func TestEnableShadowsTogglesAtRuntime(t *testing.T) {
	r, err := NewOffscreenRenderer(160, 160)
	if err != nil {
		t.Fatal(err)
	}
	defer r.Destroy()
	r.SetClearColor(colors.RGBA32F{0, 0, 0, 1})

	scene := r.NewScene()
	defer scene.Destroy()
	scene.SetAmbient(colors.RGB32F{0.04, 0.04, 0.04})
	light := scene.AddDirectionalLight(glm.Vec3f{-0.4, -1, -0.3}, colors.RGB32F{1, 1, 1}, 3)
	light.SetCastShadow(true)

	cube := r.GeometryStore.Create(normalCube())
	defer cube.Release()

	ground := scene.NewMesh(cube, r.NewPBRMaterial())
	ground.SetScale(glm.Vec3f{6, 0.2, 6})
	scene.Add(ground)

	occluder := scene.NewMesh(cube, r.NewPBRMaterial())
	occluder.SetPosition(glm.Vec3f{0, 1.5, 0})
	occluder.SetScale(glm.Vec3f{0.8, 0.8, 0.8})
	occluder.SetCastShadow(true)
	scene.Add(occluder)

	cam := cameras.NewPerspectiveCamera(45, 1, 0.1, 100)
	cam.SetPosition(glm.Vec3f{0, 5, 6})
	cam.SetTarget(glm.Vec3f{0, 0, 0})

	frame := func() int64 {
		r.Render(scene, cam)
		return sceneLuma(r.Pixels())
	}

	r.EnableShadows(true)
	frame() // first frame allocates and fills the map
	on := frame()

	r.EnableShadows(false)
	off := frame()

	if off <= on {
		t.Fatalf("EnableShadows(false) did not remove the shadow: luma on=%d off=%d "+
			"(off must be brighter once the receiver is no longer occluded)", on, off)
	}

	// And back on again, so the toggle is not one-way.
	r.EnableShadows(true)
	frame()
	if again := frame(); again >= off {
		t.Fatalf("re-enabling shadows did not darken the frame: off=%d back-on=%d", off, again)
	}
}
