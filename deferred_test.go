package pix

import (
	"testing"

	"github.com/bluescreen10/pix/cameras"
	"github.com/bluescreen10/pix/colors"
	"github.com/bluescreen10/pix/glm"
	"github.com/bluescreen10/pix/gpu"
)

// TestDeferredPBRRenders is the smoke test for the G-buffer path: an opaque PBR mesh
// now renders through Deferred()+Lighting() (the G-buffer fill + a fullscreen
// lighting pass), not Forward(). It should look lit — non-black — same as before.
func TestDeferredPBRRenders(t *testing.T) {
	r, err := NewOffscreenRenderer(96, 96)
	if err != nil {
		t.Fatal(err)
	}
	defer r.Destroy()
	r.EnableDeferredRendering(true)
	r.SetClearColor(colors.RGBA32F{0, 0, 0, 1})

	scene := r.NewScene()
	defer scene.Destroy()
	scene.SetAmbient(colors.RGB32F{0.6, 0.6, 0.6})
	light := scene.AddDirectionalLight(glm.Vec3f{-0.3, -1, -0.2}, colors.RGB32F{1, 1, 1}, 2)
	_ = light

	cube := r.NewGeometry(normalCube())
	defer cube.Release()
	mat := r.NewPBRMaterial()
	if mat.Blend() != BlendOpaque {
		t.Fatalf("PBRMaterial default blend = %v, want BlendOpaque", mat.Blend())
	}
	if mat.Deferred() == nil || mat.Lighting() == nil {
		t.Fatal("PBRMaterial should provide Deferred()+Lighting() (eligible for the G-buffer path)")
	}
	scene.Add(scene.NewMesh(cube, mat))

	cam := cameras.NewPerspectiveCamera(45, 1, 0.1, 100)
	cam.SetPosition(glm.Vec3f{0, 0, 3})
	r.Render(scene, cam)

	px := r.Pixels()
	var lit bool
	for i := 0; i+3 < len(px); i += 4 {
		if px[i] > 8 || px[i+1] > 8 || px[i+2] > 8 {
			lit = true
			break
		}
	}
	if !lit {
		t.Fatal("deferred PBR cube rendered an all-black frame")
	}
}

// TestDeferredRenderingOffByDefault checks the EnableDeferredRendering gate itself: a
// PBR material is eligible for the G-buffer path (it provides Deferred+Lighting), but
// without opting in, the renderer must still route it through Forward() — same as
// before this feature existed. The scene still renders lit either way, so the
// assertion is on the pipeline choice, not just the pixels.
func TestDeferredRenderingOffByDefault(t *testing.T) {
	r, err := NewOffscreenRenderer(64, 64)
	if err != nil {
		t.Fatal(err)
	}
	defer r.Destroy()
	// Deliberately NOT calling EnableDeferredRendering.

	scene := r.NewScene()
	defer scene.Destroy()
	scene.SetAmbient(colors.RGB32F{0.6, 0.6, 0.6})

	cube := r.NewGeometry(normalCube())
	defer cube.Release()
	mat := r.NewPBRMaterial()
	scene.Add(scene.NewMesh(cube, mat))

	cam := cameras.NewPerspectiveCamera(45, 1, 0.1, 100)
	cam.SetPosition(glm.Vec3f{0, 0, 3})
	r.Render(scene, cam)

	if got := r.pipelineForMaterial(mat); r.drawPipelineKeys[got].pass != passForward {
		t.Fatalf("deferred rendering off but material routed to pass %v, want passForward", r.drawPipelineKeys[got].pass)
	}
}

// TestDeferredAndForwardMixed renders a deferred PBR cube (left) alongside a
// forward-only BasicMaterial cube (right, unlit) in the same frame, proving the
// G-buffer pass and the forward pass correctly share one color+depth target: the
// forward pass must load (not clear) what the G-buffer + lighting passes wrote.
func TestDeferredAndForwardMixed(t *testing.T) {
	r, err := NewOffscreenRenderer(160, 80)
	if err != nil {
		t.Fatal(err)
	}
	defer r.Destroy()
	r.EnableDeferredRendering(true)
	r.SetClearColor(colors.RGBA32F{0, 0, 0, 1})

	scene := r.NewScene()
	defer scene.Destroy()
	scene.SetAmbient(colors.RGB32F{0.8, 0.8, 0.8})

	cube := r.NewGeometry(normalCube())
	defer cube.Release()

	pbr := r.NewPBRMaterial()
	pbr.SetColor(colors.RGBA32F{1, 1, 1, 1})
	left := scene.NewMesh(cube, pbr)
	left.SetPosition(glm.Vec3f{-1.2, 0, 0})
	scene.Add(left)

	basic := r.NewBasicMaterial()
	basic.SetColor(colors.RGBA32F{0, 1, 0, 1})
	if basic.Deferred() != nil || basic.Lighting() != nil {
		t.Fatal("BasicMaterial should be forward-only (no Deferred/Lighting)")
	}
	right := scene.NewMesh(cube, basic)
	right.SetPosition(glm.Vec3f{1.2, 0, 0})
	scene.Add(right)

	cam := cameras.NewPerspectiveCamera(60, 2, 0.1, 100)
	cam.SetPosition(glm.Vec3f{0, 0, 4})
	r.Render(scene, cam)

	px := r.Pixels()
	w, h := 160, 80
	// Left half should show the lit (deferred) PBR cube (white-ish, R≈G≈B); right half
	// the unlit green (forward) BasicMaterial cube (G only) — scan each half for its
	// expected color rather than probing one pixel, since the cube's exact silhouette
	// at this camera distance isn't worth hand-computing.
	var leftWhite, rightGreen int
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			i := (y*w + x) * 4
			rr, gg, bb := px[i], px[i+1], px[i+2]
			if x < w/2 && rr > 100 && gg > 100 && bb > 100 {
				leftWhite++
			}
			if x >= w/2 && gg > 100 && rr < 40 && bb < 40 {
				rightGreen++
			}
		}
	}
	t.Logf("left(deferred, white) px=%d right(forward, green) px=%d", leftWhite, rightGreen)
	if leftWhite == 0 {
		t.Fatal("left (deferred PBR) cube not found — G-buffer/lighting pass produced nothing")
	}
	if rightGreen == 0 {
		t.Fatal("right (forward Basic) cube not found — forward pass didn't render, or the G-buffer pass clobbered it")
	}
}

// TestDeferredEmissiveMatchesForward guards the emissive encoding path. Emissive rides
// in the G-buffer specifically so the lighting pass can add it to the lit result and
// sRGB-encode the sum once. Encoding emissive and lighting separately and blending them
// (the earlier approach) is badly wrong — srgb(a)+srgb(b) is far brighter than
// srgb(a+b), and two moderate terms clip to white — so deferred and forward must agree.
func TestDeferredEmissiveMatchesForward(t *testing.T) {
	render := func(deferred bool) []byte {
		r, err := NewOffscreenRenderer(64, 64)
		if err != nil {
			t.Fatal(err)
		}
		defer r.Destroy()
		r.EnableDeferredRendering(deferred)
		r.SetClearColor(colors.RGBA32F{0, 0, 0, 1})

		scene := r.NewScene()
		defer scene.Destroy()
		scene.SetAmbient(colors.RGB32F{0.25, 0.25, 0.25})

		cube := r.NewGeometry(normalCube())
		defer cube.Release()
		mat := r.NewPBRMaterial()
		mat.SetColor(colors.RGBA32F{1, 1, 1, 1})
		mat.SetEmissive(colors.RGB32F{0.25, 0.25, 0.25})
		scene.Add(scene.NewMesh(cube, mat))

		cam := cameras.NewPerspectiveCamera(45, 1, 0.1, 100)
		cam.SetPosition(glm.Vec3f{0, 0, 3})
		r.Render(scene, cam)
		out := make([]byte, len(r.Pixels()))
		copy(out, r.Pixels())
		return out
	}

	fwd, def := render(false), render(true)
	// Compare the brightest lit pixel each path produced: a double-encode blows the
	// deferred value out toward 255 while forward stays mid-grey.
	maxOf := func(px []byte) int {
		m := 0
		for i := 0; i+3 < len(px); i += 4 {
			if int(px[i]) > m {
				m = int(px[i])
			}
		}
		return m
	}
	f, d := maxOf(fwd), maxOf(def)
	t.Logf("brightest red: forward=%d deferred=%d", f, d)
	// 8-bit G-buffer quantization of albedo/emissive allows a few LSB of drift.
	if diff := f - d; diff > 4 || diff < -4 {
		t.Fatalf("deferred emissive diverges from forward: forward=%d deferred=%d", f, d)
	}
}

// TestMaterialStoreDedupsOnWholeShader checks that stores are keyed on the entire
// Shader, not just Forward. Sharing a store means sharing its Shader with every
// instance, so matching on Forward alone would hand a forward-only material another
// Shader's Deferred/Lighting (routing it through the G-buffer against a record layout
// it never declared) — or strip PBR's deferred path, depending on creation order.
func TestMaterialStoreDedupsOnWholeShader(t *testing.T) {
	r, err := NewOffscreenRenderer(16, 16)
	if err != nil {
		t.Fatal(err)
	}
	defer r.Destroy()

	full := r.materials.store(
		Shader{Forward: pbrForwardSPV, Deferred: pbrDeferredSPV, Lighting: pbrLightingSPV},
		"full")
	// Same Forward shader, but no deferred path — must NOT share the store above.
	forwardOnly := r.materials.store(Shader{Forward: pbrForwardSPV}, "forward-only")

	if full == forwardOnly {
		t.Fatal("stores with different Deferred/Lighting shaders were deduped together")
	}
	if forwardOnly.sh.Deferred != nil || forwardOnly.sh.Lighting != nil {
		t.Fatal("forward-only store inherited another Shader's deferred path")
	}
	if full.sh.Deferred == nil || full.sh.Lighting == nil {
		t.Fatal("PBR store lost its deferred path")
	}
}

// TestDeferredBackgroundKeepsClearColor pins the behaviour the lighting pass's
// read-only depth test depends on: pixels with no geometry are rejected by the
// hardware (their depth is still the far clear value) and must therefore keep the
// scene's clear color untouched, while geometry pixels get shaded. A broken depth
// test shows up here immediately — rejecting everything leaves an all-clear frame,
// rejecting nothing lets the lighting pass overwrite the background.
func TestDeferredBackgroundKeepsClearColor(t *testing.T) {
	r, err := NewOffscreenRenderer(96, 96)
	if err != nil {
		t.Fatal(err)
	}
	defer r.Destroy()
	r.EnableDeferredRendering(true)
	// A distinctive clear color: neither black nor anything the lit cube produces.
	r.SetClearColor(colors.RGBA32F{0, 0, 1, 1})

	scene := r.NewScene()
	defer scene.Destroy()
	scene.SetAmbient(colors.RGB32F{0.9, 0.9, 0.9})

	cube := r.NewGeometry(normalCube())
	defer cube.Release()
	mat := r.NewPBRMaterial()
	mat.SetColor(colors.RGBA32F{1, 0, 0, 1}) // red cube on a blue background
	scene.Add(scene.NewMesh(cube, mat))

	cam := cameras.NewPerspectiveCamera(45, 1, 0.1, 100)
	cam.SetPosition(glm.Vec3f{0, 0, 3})
	r.Render(scene, cam)

	px := r.Pixels()
	var blueBg, redLit int
	for i := 0; i+3 < len(px); i += 4 {
		rr, gg, bb := px[i], px[i+1], px[i+2]
		if bb > 100 && rr < 40 && gg < 40 {
			blueBg++
		}
		if rr > 100 && gg < 60 && bb < 60 {
			redLit++
		}
	}
	t.Logf("background(blue) px=%d lit(red) px=%d", blueBg, redLit)
	if blueBg == 0 {
		t.Fatal("background lost its clear color — the lighting pass shaded pixels with no geometry")
	}
	if redLit == 0 {
		t.Fatal("no lit geometry — the depth test rejected pixels that do have geometry")
	}
}

// TestDrawListBuffersGrowOnly pins ensureBuffers' allocation policy: a rebuild that
// doesn't need more room must reuse the existing buffers rather than free and
// reallocate them. Toggling a material's blend mode changes its pipeline assignment,
// which forces a structural rebuild without changing any of the counts.
func TestDrawListBuffersGrowOnly(t *testing.T) {
	r, err := NewOffscreenRenderer(64, 64)
	if err != nil {
		t.Fatal(err)
	}
	defer r.Destroy()

	scene := r.NewScene()
	defer scene.Destroy()
	scene.SetAmbient(colors.RGB32F{0.8, 0.8, 0.8})

	cube := r.NewGeometry(normalCube())
	defer cube.Release()
	mat := r.NewPBRMaterial()
	scene.Add(scene.NewMesh(cube, mat))

	cam := cameras.NewPerspectiveCamera(45, 1, 0.1, 100)
	cam.SetPosition(glm.Vec3f{0, 0, 3})
	r.Render(scene, cam)

	dl := scene.drawList
	before := [...]gpu.Handle{
		dl.drawableBuf.H, dl.indirectBuf.H, dl.regionBuf.H, dl.visibleBuf.H,
		dl.drawRootBuf.H, dl.cullRootBuf.H, dl.lightingRootBuf.H,
	}

	// Force a rebuild (new pipeline assignment) that needs no extra space.
	mat.SetBlend(BlendAlpha)
	r.Render(scene, cam)

	after := [...]gpu.Handle{
		dl.drawableBuf.H, dl.indirectBuf.H, dl.regionBuf.H, dl.visibleBuf.H,
		dl.drawRootBuf.H, dl.cullRootBuf.H, dl.lightingRootBuf.H,
	}
	names := [...]string{"drawable", "indirect", "region", "visible", "drawRoot", "cullRoot", "lightingRoot"}
	for i := range before {
		if before[i] != after[i] {
			t.Errorf("%s buffer was reallocated on a rebuild that needed no more room", names[i])
		}
	}
}
