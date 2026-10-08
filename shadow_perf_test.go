package pix_test

import (
	"testing"

	"github.com/bluescreen10/pix"
	"github.com/bluescreen10/pix/colors"
	"github.com/bluescreen10/pix/glm"
	"github.com/bluescreen10/pix/materials"
	"github.com/bluescreen10/pix/scenes"
)

// TestSurfacesFacingAwayAreUnlit pins the property the shading loop's early-out depends
// on. A surface turned away from a light receives nothing from it, so the loop skips the
// light entirely — and with it the shadow lookup, which is up to nine texture fetches for
// a result that was about to be multiplied by zero.
//
// That is only sound if such a surface really does end up unlit. If any term survived a
// negative N·L — a stray specular highlight, say — skipping would change the image
// rather than just save time, so this checks the shading rather than the saving.
func TestSurfacesFacingAwayAreUnlit(t *testing.T) {
	const size = 96

	// One slab facing the camera, lit either from the camera's side or from behind it.
	// Two renders rather than two surfaces: a box seen from any angle shows several
	// faces at once, and this needs to know which one it is looking at.
	lumFor := func(lightDir glm.Vec3f, shadows bool) int64 {
		r, err := pix.NewOffscreenRenderer(size, size)
		if err != nil {
			t.Fatal(err)
		}
		defer r.Destroy()
		r.SetClearColor(colors.RGBA32F{0, 0, 0, 1})
		r.EnableShadows(shadows)

		scene := scenes.New()
		defer scene.Destroy()
		// No ambient: anything not lit directly has to come out black, which is what
		// makes "unlit" measurable at all.
		scene.SetAmbient(colors.RGB32F{0, 0, 0}, 1)
		light := scene.AddDirectionalLight(lightDir, colors.RGB32F{1, 1, 1}, 3)
		light.SetCastShadow(true)
		light.Shadow().SetCascades(3)

		cube := r.GeometryStore.Create(pix.BoxGeometry(1, 1, 1))
		defer cube.Release()
		slab := scene.NewMesh(cube, r.NewPBRMaterial())
		slab.SetScale(glm.Vec3f{4, 4, 0.2})
		scene.Add(slab)

		cam := scene.NewPerspectiveCamera(50, 1, 0.1, 50)
		scene.Add(cam)
		cam.SetPosition(glm.Vec3f{0, 0, 6})
		cam.LookAt(glm.Vec3f{0, 0, 0})
		r.Render(scene)

		px := r.Pixels()
		var sum int64
		for i := 0; i+3 < len(px); i += 4 {
			sum += int64(px[i]) + int64(px[i+1]) + int64(px[i+2])
		}
		return sum
	}

	for _, shadows := range []bool{false, true} {
		// Travelling -Z reaches the +Z face the camera is looking at; travelling +Z
		// leaves that face turned away.
		lit := lumFor(glm.Vec3f{0, 0, -1}, shadows)
		away := lumFor(glm.Vec3f{0, 0, 1}, shadows)
		if lit == 0 {
			t.Fatalf("shadows=%v: the face toward the light is black; the test measures nothing", shadows)
		}
		if away != 0 {
			t.Errorf("shadows=%v: a face turned away from the light sums to %d, want 0; the "+
				"shading loop skips those lights outright, so anything still shading them "+
				"would be dropped rather than merely made cheaper", shadows, away)
		}
	}
}

// BenchmarkFrame reports GPU time per pass for a scene with a few thousand casters. Run
// it with -benchtime=3x or more; the numbers that matter are logged, not the ns/op,
// since what is being measured happens on the GPU after the Go call returns.
func BenchmarkFrame(b *testing.B) {
	const size = 1280
	r, err := pix.NewOffscreenRenderer(size, size)
	if err != nil {
		b.Fatal(err)
	}
	defer r.Destroy()
	r.ShowFPS(true) // what drives the GPU timestamps
	r.EnableShadows(true)

	scene := scenes.New()
	defer scene.Destroy()
	scene.SetAmbient(colors.RGB32F{0.3, 0.3, 0.35}, 1)
	light := scene.AddDirectionalLight(glm.Vec3f{-0.5, -1, -0.35}, colors.RGB32F{1, 0.96, 0.9}, 2)
	light.SetCastShadow(true)
	light.Shadow().SetCascades(4)

	cube := r.GeometryStore.Create(pix.BoxGeometry(1, 1, 1))
	defer cube.Release()
	ground := scene.NewMesh(cube, r.NewPBRMaterial())
	ground.SetScale(glm.Vec3f{600, 0.4, 600})
	scene.Add(ground)
	for i := range 900 {
		p := scene.NewMesh(cube, r.NewPBRMaterial())
		p.SetPosition(glm.Vec3f{float32(i%30)*12 - 180, 3 + float32(i%7), -float32(i/30) * 12})
		p.SetScale(glm.Vec3f{2 + float32(i%3), 6 + float32(i%11), 2 + float32(i%5)})
		p.SetCastShadow(true)
		scene.Add(p)
	}

	cam := scene.NewPerspectiveCamera(55, 1, 0.3, 2000)
	scene.Add(cam)
	cam.SetPosition(glm.Vec3f{0, 6, 40})
	cam.LookAt(glm.Vec3f{0, 3, -40})

	for b.Loop() {
		r.Render(scene)
	}
	b.StopTimer()
	for _, p := range []pix.GPUPass{pix.GPUPassCull, pix.GPUPassShadow, pix.GPUPassPrepass, pix.GPUPassOpaque, pix.GPUPassAmbientOcclusion, pix.GPUPassTransparent, pix.GPUPassPostProcessing} {
		if d := r.Profiler().PassTime(p); d > 0 {
			b.Logf("%-8s %.3f ms", p, float64(d.Microseconds())/1000)
		}
	}
	b.Logf("%-8s %.3f ms", "TOTAL", float64(r.Profiler().GPUTime().Microseconds())/1000)
}

// TestDepthPrepassDoesNotChangeTheImage. A depth prepass is purely a performance
// bargain: fill depth first so the shading pass runs once per visible pixel instead of
// once per fragment drawn. Whether it pays depends on the hardware, but it must never
// change what is drawn, so this pins the part that is not a trade-off.
//
// The trap it guards is the depth comparison. With a prepass the shading draw meets
// depth it wrote itself, so a strict Greater test (reversed-Z) rejects every fragment
// and the scene renders empty — a failure that looks like the geometry vanished rather
// than like a depth-state bug.
func TestDepthPrepassDoesNotChangeTheImage(t *testing.T) {
	const size = 220

	render := func(prepass bool) []byte {
		r, err := pix.NewOffscreenRenderer(size, size)
		if err != nil {
			t.Fatal(err)
		}
		defer r.Destroy()
		r.SetClearColor(colors.RGBA32F{0.5, 0.6, 0.8, 1})
		r.EnableShadows(true)
		r.EnableDepthPrepass(prepass)

		scene := scenes.New()
		defer scene.Destroy()
		scene.SetAmbient(colors.RGB32F{0.2, 0.2, 0.25}, 1)
		light := scene.AddDirectionalLight(glm.Vec3f{-0.4, -1, -0.3}, colors.RGB32F{1, 1, 1}, 3)
		light.SetCastShadow(true)
		light.Shadow().SetCascades(3)

		cube := r.GeometryStore.Create(pix.BoxGeometry(1, 1, 1))
		defer cube.Release()
		ground := scene.NewMesh(cube, r.NewPBRMaterial())
		ground.SetScale(glm.Vec3f{80, 0.4, 80})
		scene.Add(ground)
		// Deliberately overlapping from the camera's side, so a prepass has something to
		// reject and any depth-state mistake shows as missing geometry.
		for i := range 24 {
			b := scene.NewMesh(cube, r.NewPBRMaterial())
			b.SetPosition(glm.Vec3f{float32(i%5)*3 - 6, 2, -float32(i) * 2})
			b.SetScale(glm.Vec3f{3, 4, 3})
			b.SetCastShadow(true)
			scene.Add(b)
		}

		cam := scene.NewPerspectiveCamera(55, 1, 0.3, 300)
		scene.Add(cam)
		cam.SetPosition(glm.Vec3f{0, 8, 22})
		cam.LookAt(glm.Vec3f{0, 2, -20})
		r.Render(scene)
		return append([]byte(nil), r.Pixels()...)
	}

	plain, prepassed := render(false), render(true)

	var lit, differing int
	for i := 0; i+3 < len(plain); i += 4 {
		if plain[i] > 12 {
			lit++
		}
		// A couple of levels of slack: exactly coplanar surfaces are settled by draw
		// order under GreaterEqual, and which one wins was never defined.
		d := int(plain[i]) - int(prepassed[i])
		if d < 0 {
			d = -d
		}
		if d > 2 {
			differing++
		}
	}
	if lit == 0 {
		t.Fatal("the reference frame is empty; the test measures nothing")
	}
	if pct := 100 * float64(differing) / float64(len(plain)/4); pct > 1 {
		t.Errorf("%.2f%% of pixels changed with the depth prepass on; it is meant to change "+
			"only what the frame costs, not what it shows", pct)
	}
}

// TestDepthPrepassRespectsCulling pins the failure a whole-image comparison misses.
//
// A depth prepass must cull exactly as the shading pass will. Sharing one double-sided
// pipeline across materials that cull back faces writes depth for faces the shading pass
// then discards — so a surface the viewer was never meant to see occludes everything
// behind it. On a closed mesh nothing shows, because the front face wins the depth test
// anyway; it only appears where single-sided geometry is seen from behind, which is why
// it survives a diff of a scene full of solid objects and then ruins a real one.
//
// The setup is that case exactly: a back-facing wall between the camera and a box.
func TestDepthPrepassRespectsCulling(t *testing.T) {
	const size = 128

	litPixels := func(prepass bool) int {
		r, err := pix.NewOffscreenRenderer(size, size)
		if err != nil {
			t.Fatal(err)
		}
		defer r.Destroy()
		r.SetClearColor(colors.RGBA32F{0, 0, 0, 1})
		r.EnableShadows(true)
		r.EnableDepthPrepass(prepass)

		scene := scenes.New()
		defer scene.Destroy()
		scene.SetAmbient(colors.RGB32F{0.5, 0.5, 0.5}, 1)
		light := scene.AddDirectionalLight(glm.Vec3f{0, -1, -0.4}, colors.RGB32F{1, 1, 1}, 2)
		light.SetCastShadow(true)
		light.Shadow().SetCascades(3)

		quad := r.GeometryStore.Create(pix.PlaneGeometry(40, 40, 1, 1))
		defer quad.Release()
		cube := r.GeometryStore.Create(pix.BoxGeometry(6, 6, 6))
		defer cube.Release()

		// The box, then a single-sided wall in front of it turned away from the camera.
		box := scene.NewMesh(cube, r.NewPBRMaterial())
		box.SetPosition(glm.Vec3f{0, 0, -20})
		scene.Add(box)

		wallMat := r.NewPBRMaterial()
		wallMat.SetCull(materials.CullBack)
		wall := scene.NewMesh(quad, wallMat)
		wall.SetPosition(glm.Vec3f{0, 0, -10})
		wall.SetRotation(glm.Vec3f{1.5707963, 0, 0}) // facing away from the camera
		scene.Add(wall)

		cam := scene.NewPerspectiveCamera(50, 1, 0.5, 200)
		scene.Add(cam)
		cam.SetPosition(glm.Vec3f{0, 0, 10})
		cam.LookAt(glm.Vec3f{0, 0, -20})
		r.Render(scene)

		px := r.Pixels()
		n := 0
		for i := 0; i+3 < len(px); i += 4 {
			if int(px[i])+int(px[i+1])+int(px[i+2]) > 30 {
				n++
			}
		}
		return n
	}

	plain := litPixels(false)
	if plain == 0 {
		t.Fatal("nothing is visible without the prepass; the test measures nothing")
	}
	if got := litPixels(true); got < plain*3/4 {
		t.Errorf("the depth prepass hid geometry: %d pixels visible against %d without it. "+
			"It is writing depth for faces the shading pass culls, so a surface that is "+
			"never drawn is occluding what is behind it", got, plain)
	}
}

// TestDepthDebugViewIsMaterialIndependent: an unlit box and a PBR one must register
// equally in the depth view, because the view is about geometry and neither material
// has anything to say about depth.
//
// It used to be untrue. The views were re-reads of the G-buffer, so a material with no
// deferred path — anything unlit — was simply missing from them and read as geometry
// that had failed to draw at all, which is the exact question someone opens the depth
// view to answer. Every view is a geometry pass over its own fragment shader now, so
// material type cannot affect what appears; this holds that.
func TestDepthDebugViewIsMaterialIndependent(t *testing.T) {
	const size = 160

	// A tall unlit box against the far plane: in the depth view it should read as
	// markedly nearer than the empty background behind it.
	depthViewOf := func(box string) []byte {
		r, err := pix.NewOffscreenRenderer(size, size)
		if err != nil {
			t.Fatal(err)
		}
		defer r.Destroy()
		r.SetClearColor(colors.RGBA32F{0, 0, 0, 1})
		r.SetDebugView(pix.DebugDepth)

		scene := scenes.New()
		defer scene.Destroy()
		scene.SetAmbient(colors.RGB32F{0.3, 0.3, 0.3}, 1)
		scene.AddDirectionalLight(glm.Vec3f{0.2, -1, 0.3}, colors.RGB32F{1, 1, 1}, 2)

		cube := r.GeometryStore.Create(pix.BoxGeometry(1, 1, 1))
		defer cube.Release()

		floor := scene.NewMesh(cube, r.NewPBRMaterial())
		floor.SetPosition(glm.Vec3f{0, -4, 0})
		floor.SetScale(glm.Vec3f{60, 0.5, 60})
		scene.Add(floor)

		var mat materials.Material
		switch box {
		case "unlit":
			mat = r.NewBasicMaterial()
		case "pbr":
			mat = r.NewPBRMaterial()
		}
		if mat != nil {
			m := scene.NewMesh(cube, mat)
			m.SetPosition(glm.Vec3f{0, 0, 0})
			m.SetScale(glm.Vec3f{6, 6, 6})
			scene.Add(m)
		}

		cam := scene.NewPerspectiveCamera(50, 1, 0.5, 120)
		scene.Add(cam)
		cam.SetPosition(glm.Vec3f{0, 2, 18})
		cam.LookAt(glm.Vec3f{0, 0, 0})
		r.Render(scene)
		return append([]byte(nil), r.Pixels()...)
	}

	// The PBR box is the reference for what the view should look like.
	noBox := depthViewOf("none")
	pbrView := depthViewOf("pbr")
	unlitView := depthViewOf("unlit")

	// How many pixels the box CHANGED versus a frame without it. Absolute coverage says
	// nothing, because the floor fills the same region either way — what identifies the
	// box in a depth view is that it is nearer than what it stands in front of.
	changed := func(px []byte) int {
		n := 0
		for i := 0; i+3 < len(px); i += 4 {
			d := int(px[i]) - int(noBox[i])
			if d < 0 {
				d = -d
			}
			if d > 8 {
				n++
			}
		}
		return n
	}

	want, got := changed(pbrView), changed(unlitView)
	if want == 0 {
		t.Fatal("the PBR box is absent from its own depth view; the test measures nothing")
	}
	if got < want/2 {
		t.Errorf("the unlit box covers %d px of the depth view against the PBR box's %d; "+
			"the view is dropping geometry by material type", got, want)
	}
}
