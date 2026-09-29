package pix_test

import (
	"bytes"
	"testing"

	"github.com/bluescreen10/pix"
)

// TestDebugViewNamesRoundTrip: the console addresses views by name, so parse and String
// must agree for every one of them — a mismatch means `set debug normal` reports back
// something else.
func TestDebugViewNamesRoundTrip(t *testing.T) {
	for _, name := range pix.DebugViewNames() {
		v, ok := pix.ParseDebugView(name)
		if !ok {
			t.Errorf("ParseDebugView(%q) failed on a name it published", name)
			continue
		}
		if got := v.String(); got != name {
			t.Errorf("%q parsed to %v which stringifies as %q", name, uint32(v), got)
		}
	}
	if _, ok := pix.ParseDebugView("nonsense"); ok {
		t.Error("an unknown view name was accepted")
	}
	if got := pix.DebugOff.String(); got != "off" {
		t.Errorf("DebugOff = %q, want off", got)
	}
}

// TestEveryDebugViewChangesTheFrame: a view is a geometry pass with its own fragment
// shader, so every one of them must visibly replace the shaded frame — for any
// material, with nothing else switched on first.
//
// They used to be re-reads of the G-buffer and so lay dormant unless deferred rendering
// was enabled, which is exactly the coupling this removes.
func TestEveryDebugViewChangesTheFrame(t *testing.T) {
	r, scene := shotScene(t, 32, 32)

	r.SetDebugView(pix.DebugOff)
	r.Render(scene)
	shaded := append([]byte(nil), r.Pixels()...)

	for _, name := range pix.DebugViewNames() {
		v, _ := pix.ParseDebugView(name)
		if v == pix.DebugOff {
			continue
		}
		r.SetDebugView(v)
		r.Render(scene)
		if bytes.Equal(shaded, r.Pixels()) {
			t.Errorf("%v produced the shaded frame unchanged", v)
		}
	}
}

// TestDebugViewsRenderDistinctFrames drives every view through a real frame. Each shows
// different data, so each must produce a different image — and none may be blank, which
// is what a wrong shader or an unfilled root would look like.
func TestDebugViewsRenderDistinctFrames(t *testing.T) {
	r, scene := shotScene(t, 96, 96)

	// The frames are compared as pixels, not as a summed luma. A sum is a lossy hash,
	// and two of these views genuinely collided on one while rendering different
	// images — the test reported a bug that wasn't there.
	frame := func(v pix.DebugView) (px []byte, nonBlank bool) {
		r.SetDebugView(v)
		r.Render(scene)
		px = append([]byte(nil), r.Pixels()...)
		for i := 0; i+3 < len(px); i += 4 {
			if px[i] > 8 || px[i+1] > 8 || px[i+2] > 8 {
				return px, true
			}
		}
		return px, false
	}

	shaded, _ := frame(pix.DebugOff)
	seen := map[pix.DebugView][]byte{pix.DebugOff: shaded}

	for _, v := range []pix.DebugView{pix.DebugNormal, pix.DebugDepth, pix.DebugPosition, pix.DebugObjectID, pix.DebugTriangleID} {
		px, nonBlank := frame(v)
		if !nonBlank {
			t.Errorf("%v rendered a blank frame", v)
		}
		for prev, prevPx := range seen {
			if bytes.Equal(px, prevPx) {
				t.Errorf("%v produced the same image as %v — it is probably running the wrong shader", v, prev)
			}
		}
		seen[v] = px
	}

	// Turning it off must restore the shaded frame exactly.
	if again, _ := frame(pix.DebugOff); !bytes.Equal(again, shaded) {
		t.Error("returning to DebugOff did not reproduce the shaded frame")
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
	r, scene := shotScene(t, 64, 64)
	r.ShowFPS(true) // arms the GPU timestamp queries

	for _, v := range []pix.DebugView{pix.DebugOff, pix.DebugNormal, pix.DebugObjectID, pix.DebugDepth, pix.DebugOff} {
		r.SetDebugView(v)
		for range 3 { // several frames: the read happens at the end of each
			r.Render(scene)
		}
	}
}

// TestDebugViewKeepsTheOverlay: the console has to stay visible while a view is up, or
// there is no way to type the command that turns it off again. The overlay is drawn by
// the forward pass, which the debug path must therefore not skip — checked by comparing
// a frame with the stats HUD on against one with it off: if the overlay pass were being
// skipped, toggling the HUD would make no visible difference.
func TestDebugViewKeepsTheOverlay(t *testing.T) {
	r, scene := shotScene(t, 64, 64)
	r.SetDebugView(pix.DebugNormal)

	r.ShowFPS(false)
	r.Render(scene)
	withoutHUD := append([]byte(nil), r.Pixels()...)

	r.ShowFPS(true)
	r.Render(scene)
	withHUD := r.Pixels()

	if bytes.Equal(withoutHUD, withHUD) {
		t.Fatal("enabling the HUD made no visible difference while a debug view was up — " +
			"the overlay pass is being skipped, leaving no way to turn the view off")
	}
}
