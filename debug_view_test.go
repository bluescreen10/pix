package pix

import (
	"testing"
)

// TestDebugViewNamesRoundTrip: the console addresses views by name, so parse and String
// must agree for every one of them — a mismatch means `set gbuffer normal` reports back
// something else.
func TestDebugViewNamesRoundTrip(t *testing.T) {
	for _, name := range DebugViewNames() {
		v, ok := ParseDebugView(name)
		if !ok {
			t.Errorf("ParseDebugView(%q) failed on a name it published", name)
			continue
		}
		if got := v.String(); got != name {
			t.Errorf("%q parsed to %v which stringifies as %q", name, uint32(v), got)
		}
	}
	if _, ok := ParseDebugView("nonsense"); ok {
		t.Error("an unknown view name was accepted")
	}
	if got := DebugOff.String(); got != "off" {
		t.Errorf("DebugOff = %q, want off", got)
	}
}

// TestDebugViewNeedsDeferred: the views show the deferred path's intermediate targets,
// which the forward path never fills — so the setting must lie dormant rather than
// producing a black or garbage frame.
func TestDebugViewNeedsDeferred(t *testing.T) {
	r, err := NewOffscreenRenderer(32, 32)
	if err != nil {
		t.Fatal(err)
	}
	defer r.Destroy()

	r.SetDebugView(DebugNormal)
	r.EnableDeferredRendering(false)
	if r.debugViewActive() {
		t.Error("a debug view claimed to be active with deferred rendering off")
	}
	r.EnableDeferredRendering(true)
	if !r.debugViewActive() {
		t.Error("a debug view is not active with deferred rendering on")
	}
	r.SetDebugView(DebugOff)
	if r.debugViewActive() {
		t.Error("DebugOff still counts as active")
	}
}

// TestDebugViewsRenderDistinctFrames drives every view through a real deferred frame.
// Each shows different data, so each must produce a different image — and none may be
// blank, which is what a broken target index or an unbound sampler would look like.
func TestDebugViewsRenderDistinctFrames(t *testing.T) {
	r, scene, cam := shotScene(t, 96, 96)
	r.EnableDeferredRendering(true)

	frame := func(v DebugView) (sum int64, nonBlank bool) {
		r.SetDebugView(v)
		r.Render(scene, cam)
		px := r.Pixels()
		var lit int
		for i := 0; i+3 < len(px); i += 4 {
			sum += int64(px[i]) + int64(px[i+1]) + int64(px[i+2])
			if px[i] > 8 || px[i+1] > 8 || px[i+2] > 8 {
				lit++
			}
		}
		return sum, lit > 0
	}

	shaded, _ := frame(DebugOff)
	seen := map[int64]DebugView{shaded: DebugOff}

	// DebugEmissive is left out on purpose: nothing in this scene emits, so its target
	// is legitimately black and the not-blank check below would fail on correct output.
	for _, v := range []DebugView{DebugAlbedo, DebugNormal, DebugMaterial, DebugDepth, DebugPosition} {
		sum, nonBlank := frame(v)
		if !nonBlank {
			t.Errorf("%v rendered a blank frame", v)
		}
		if prev, dup := seen[sum]; dup {
			t.Errorf("%v produced the same image as %v — it is probably showing the wrong target", v, prev)
		}
		seen[sum] = v
		t.Logf("%-9s luma sum %d", v, sum)
	}

	// Turning it off must restore the shaded frame exactly.
	r.SetDebugView(DebugOff)
	r.Render(scene, cam)
	if again, _ := frame(DebugOff); again != shaded {
		t.Errorf("returning to DebugOff did not reproduce the shaded frame: %d vs %d", again, shaded)
	}
}

// TestDebugViewWithStatsCompletesFrames is the regression for a hang: the debug pass
// used to return early from encode, which skipped the closing GPU timestamp. The query
// had been reset but was never written, and readGPU's blocking read then waited on it
// forever — the app froze with the normals still on screen and no input.
//
// ShowFPS is what arms the timestamps, so it is essential to the reproduction. If this
// regresses the test hangs rather than failing, and `go test` kills it on timeout.
func TestDebugViewWithStatsCompletesFrames(t *testing.T) {
	r, scene, cam := shotScene(t, 64, 64)
	r.EnableDeferredRendering(true)
	r.ShowFPS(true) // arms the GPU timestamp queries

	for _, v := range []DebugView{DebugOff, DebugNormal, DebugAlbedo, DebugDepth, DebugOff} {
		r.SetDebugView(v)
		for range 3 { // several frames: the read happens at the end of each
			r.Render(scene, cam)
		}
	}
}

// TestDebugViewKeepsTheOverlay: the console has to stay visible while a view is up, or
// there is no way to type the command that turns it off again. The overlay is drawn by
// the forward pass, which the debug path must therefore not skip.
func TestDebugViewKeepsTheOverlay(t *testing.T) {
	r, scene, cam := shotScene(t, 64, 64)
	r.EnableDeferredRendering(true)
	r.ShowFPS(true)

	r.SetDebugView(DebugNormal)
	r.Render(scene, cam)

	if r.overlay == nil {
		t.Fatal("no overlay was created")
	}
	if len(r.overlay.quads) == 0 {
		t.Fatal("the overlay drew nothing while a debug view was up — the HUD and " +
			"console would be invisible, leaving no way to turn the view off")
	}
}
